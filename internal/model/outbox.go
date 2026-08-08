package model

import "time"

// OutboxMessage 事务发件箱模型，用于可靠地异步更新缓存。
type OutboxMessage struct {
	ID          uint64     `gorm:"primaryKey;comment:自增主键"`
	ShortCode   string     `gorm:"type:varchar(10);not null;comment:短码"`
	LongURL     string     `gorm:"type:varchar(2048);not null;comment:原始长链接"`
	URLHash     string     `gorm:"type:char(32);not null;comment:long_url的MD5哈希"`
	Status      string     `gorm:"type:varchar(20);not null;default:'pending';index:idx_outbox_status_locked_until_created_at,priority:1;index:idx_outbox_status_updated_at,priority:1;comment:消息状态 pending/processing/sent/failed"`
	RetryCount  int        `gorm:"not null;default:0;comment:消息重试次数"`
	WorkerID    string     `gorm:"type:varchar(128);not null;default:'';index;comment:领取消息的Worker"`
	LockedUntil *time.Time `gorm:"index:idx_outbox_status_locked_until_created_at,priority:2;comment:租约过期时间"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;index:idx_outbox_status_locked_until_created_at,priority:3;comment:创建时间"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime;index:idx_outbox_status_updated_at,priority:2;comment:更新时间"`
}

// TableName 指定 GORM 使用的表名
func (OutboxMessage) TableName() string {
	return "outbox_messages"
}
