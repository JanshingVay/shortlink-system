package repository

import (
	"context"
	"fmt"
	"time"

	"shortlink-system/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateOutbox 在数据库事务中创建 outbox 记录。
// 调用方需要保证传入 tx 且已在事务中。
func (s *Storage) CreateOutbox(tx *gorm.DB, msg *model.OutboxMessage) error {
	return tx.Create(msg).Error
}

// ClaimPendingOutbox 原子领取一批 outbox 消息，避免多个应用实例重复发送。
// processing 状态使用租约，Worker 崩溃后消息可以在租约到期后重新领取。
func (s *Storage) ClaimPendingOutbox(ctx context.Context, limit int, workerID string, lease time.Duration) ([]*model.OutboxMessage, error) {
	if limit <= 0 {
		return nil, nil
	}

	tx := s.MasterDB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	var msgs []*model.OutboxMessage
	now := time.Now()
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ? OR (status = ? AND locked_until < ?)", "pending", "processing", now).
		Order("created_at ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if len(msgs) == 0 {
		if err := tx.Commit().Error; err != nil {
			return nil, err
		}
		return msgs, nil
	}

	ids := make([]uint64, 0, len(msgs))
	for _, msg := range msgs {
		ids = append(ids, msg.ID)
	}
	lockedUntil := now.Add(lease)
	if err := tx.Model(&model.OutboxMessage{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"status":       "processing",
			"worker_id":    workerID,
			"locked_until": lockedUntil,
		}).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return msgs, nil
}

// MarkOutboxSent 将 outbox 记录标记为已发送。
func (s *Storage) MarkOutboxSent(ctx context.Context, id uint64) error {
	return s.MasterDB.WithContext(ctx).
		Model(&model.OutboxMessage{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":       "sent",
			"worker_id":    "",
			"locked_until": nil,
		}).Error
}

// MarkOutboxSentBatch 批量标记成功发送的消息，减少逐条 UPDATE 的数据库往返。
func (s *Storage) MarkOutboxSentBatch(ctx context.Context, ids []uint64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.MasterDB.WithContext(ctx).
		Model(&model.OutboxMessage{}).
		Where("id IN ? AND status = ? AND worker_id = ?", ids, "processing", workerID).
		Updates(map[string]interface{}{
			"status":       "sent",
			"worker_id":    "",
			"locked_until": nil,
		}).Error
}

// MarkOutboxFailed 增加重试次数，并在超过最大重试时标记为失败。
func (s *Storage) MarkOutboxFailed(ctx context.Context, id uint64, maxRetry int, workerID string) error {
	// 使用原生 SQL 原子地增加重试次数并更新状态
	sql := `
		UPDATE outbox_messages
		SET retry_count = retry_count + 1,
		    status = CASE WHEN retry_count + 1 >= ? THEN 'failed' ELSE 'pending' END,
		    worker_id = '',
		    locked_until = NULL,
		    updated_at = ?
		WHERE id = ? AND status = 'processing' AND worker_id = ?
	`
	return s.MasterDB.WithContext(ctx).Exec(sql, maxRetry, time.Now(), id, workerID).Error
}

// DeleteSentOutbox 清理已发送且超过保留时间的 outbox 记录。
func (s *Storage) DeleteSentOutbox(ctx context.Context, retain time.Duration) error {
	deadline := time.Now().Add(-retain)
	const batchSize = 1000
	var ids []uint64
	if err := s.MasterDB.WithContext(ctx).
		Model(&model.OutboxMessage{}).
		Where("status = ? AND updated_at < ?", "sent", deadline).
		Order("updated_at ASC").
		Limit(batchSize).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	result := s.MasterDB.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&model.OutboxMessage{})
	if result.Error != nil {
		return result.Error
	}
	fmt.Printf("清理 %d 条已发送的 outbox 记录\n", result.RowsAffected)
	return nil
}
