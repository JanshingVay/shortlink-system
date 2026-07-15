package repository

import (
	"context"
	"fmt"
	"time"

	"shortlink-system/internal/model"

	"gorm.io/gorm"
)

// CreateOutbox 在数据库事务中创建 outbox 记录。
// 调用方需要保证传入 tx 且已在事务中。
func (s *Storage) CreateOutbox(tx *gorm.DB, msg *model.OutboxMessage) error {
	return tx.Create(msg).Error
}

// FetchPendingOutbox 获取指定数量的待处理 outbox 消息。
func (s *Storage) FetchPendingOutbox(ctx context.Context, limit int) ([]*model.OutboxMessage, error) {
	var msgs []*model.OutboxMessage
	err := s.MasterDB.WithContext(ctx).
		Where("status = ?", "pending").
		Order("created_at ASC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

// MarkOutboxSent 将 outbox 记录标记为已发送。
func (s *Storage) MarkOutboxSent(ctx context.Context, id uint64) error {
	return s.MasterDB.WithContext(ctx).
		Model(&model.OutboxMessage{}).
		Where("id = ?", id).
		Update("status", "sent").Error
}

// MarkOutboxFailed 增加重试次数，并在超过最大重试时标记为失败。
func (s *Storage) MarkOutboxFailed(ctx context.Context, id uint64, maxRetry int) error {
	// 使用原生 SQL 原子地增加重试次数并更新状态
	sql := `
		UPDATE outbox_messages
		SET retry_count = retry_count + 1,
		    status = CASE WHEN retry_count + 1 >= ? THEN 'failed' ELSE 'pending' END,
		    updated_at = ?
		WHERE id = ?
	`
	return s.MasterDB.WithContext(ctx).Exec(sql, maxRetry, time.Now(), id).Error
}

// DeleteSentOutbox 清理已发送且超过保留时间的 outbox 记录。
func (s *Storage) DeleteSentOutbox(ctx context.Context, retain time.Duration) error {
	deadline := time.Now().Add(-retain)
	result := s.MasterDB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", "sent", deadline).
		Delete(&model.OutboxMessage{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		fmt.Printf("清理 %d 条已发送的 outbox 记录\n", result.RowsAffected)
	}
	return nil
}
