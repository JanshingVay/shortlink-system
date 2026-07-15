package worker

import (
	"context"
	"log"
	"time"

	"shortlink-system/internal/model"
	"shortlink-system/internal/repository"
	"shortlink-system/pkg/mq"
)

const (
	// outboxPollInterval 扫描 outbox 表的时间间隔
	outboxPollInterval = 1 * time.Second
	// outboxBatchSize 每次扫描的记录数
	outboxBatchSize = 100
	// outboxMaxRetry outbox 最大重试次数
	outboxMaxRetry = 3
	// outboxRetain 已发送消息保留时间
	outboxRetain = 7 * 24 * time.Hour
)

// OutboxWorker 事务发件箱 Worker
type OutboxWorker struct {
	repo   *repository.Storage
	mq     *mq.RabbitMQ
	stopCh chan struct{}
}

// NewOutboxWorker 创建 Outbox Worker
func NewOutboxWorker(repo *repository.Storage, m *mq.RabbitMQ) *OutboxWorker {
	return &OutboxWorker{
		repo:   repo,
		mq:     m,
		stopCh: make(chan struct{}),
	}
}

// Start 启动 outbox 扫描任务。
func (w *OutboxWorker) Start(ctx context.Context) {
	log.Println("[OutboxWorker] 启动")

	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	// 启动时先执行一次，快速处理积压消息
	w.processPending(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[OutboxWorker] 收到退出信号")
			return
		case <-w.stopCh:
			log.Println("[OutboxWorker] 已停止")
			return
		case <-ticker.C:
			w.processPending(ctx)
		}
	}
}

// Stop 停止 outbox 扫描任务。
func (w *OutboxWorker) Stop() {
	close(w.stopCh)
}

// processPending 处理一批 pending 的 outbox 消息。
func (w *OutboxWorker) processPending(ctx context.Context) {
	msgs, err := w.repo.FetchPendingOutbox(ctx, outboxBatchSize)
	if err != nil {
		log.Printf("[OutboxWorker] 拉取 pending 消息失败: %v", err)
		return
	}

	if len(msgs) == 0 {
		return
	}

	log.Printf("[OutboxWorker] 本次处理 %d 条 pending 消息", len(msgs))

	for _, msg := range msgs {
		if err := w.publish(ctx, msg); err != nil {
			log.Printf("[OutboxWorker] 发送消息失败 [id=%d]: %v", msg.ID, err)
			if err := w.repo.MarkOutboxFailed(ctx, msg.ID, outboxMaxRetry); err != nil {
				log.Printf("[OutboxWorker] 标记重试失败 [id=%d]: %v", msg.ID, err)
			}
			continue
		}

		if err := w.repo.MarkOutboxSent(ctx, msg.ID); err != nil {
			log.Printf("[OutboxWorker] 标记 sent 失败 [id=%d]: %v", msg.ID, err)
			continue
		}
	}

	// 定期清理已发送消息
	if err := w.repo.DeleteSentOutbox(ctx, outboxRetain); err != nil {
		log.Printf("[OutboxWorker] 清理已发送消息失败: %v", err)
	}
}

// publish 将 outbox 消息发布到 RabbitMQ。
func (w *OutboxWorker) publish(ctx context.Context, msg *model.OutboxMessage) error {
	return w.mq.Publish(ctx, mq.CacheUpdateMessage{
		ShortCode: msg.ShortCode,
		LongURL:   msg.LongURL,
		URLHash:   msg.URLHash,
	})
}
