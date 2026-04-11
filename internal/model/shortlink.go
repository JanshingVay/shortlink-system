package model

import "time"

// ShortLink 映射数据库中的短链表
type ShortLink struct {
	ID        uint64    `gorm:"primaryKey;comment:雪花算法生成的唯一ID"`
	ShortCode string    `gorm:"type:varchar(10);uniqueIndex;not null;comment:Base62短码"`
	LongURL   string    `gorm:"type:varchar(2048);not null;comment:原始长链接"`
	CreatedAt time.Time `gorm:"autoCreateTime;comment:创建时间"`
}

// TableName 指定 GORM 使用的表名
func (ShortLink) TableName() string {
	return "short_links"
}