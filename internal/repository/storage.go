package repository

import (
	"context"
	"fmt"
	"log"
	"time"

	"shortlink-system/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Storage 封装了 MySQL 和 Redis 的客户端
type Storage struct {
	DB    *gorm.DB
	Redis *redis.Client
}

// Config 数据库配置参数 (后续可由 yaml 配置文件传入)
type Config struct {
	MySQLDSN  string // 例如: "user:pass@tcp(127.0.0.1:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local"
	RedisAddr string // 例如: "127.0.0.1:6379"
	RedisPass string
}

// NewStorage 初始化并返回高并发配置的存储层
func NewStorage(cfg Config) (*Storage, error) {
	// ==========================================
	// 1. 初始化 MySQL (GORM)
	// ==========================================
	// 生产环境建议调低 GORM 的日志级别，避免大量 I/O 拖慢性能
	db, err := gorm.Open(mysql.Open(cfg.MySQLDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	// 提取底层的 sql.DB 对象，用于配置高并发连接池！
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 【面试必考：MySQL 连接池调优】
	// 设置最大打开的连接数，默认是 0 (无限制)，高并发下会撑爆 MySQL
	sqlDB.SetMaxOpenConns(100)
	// 设置最大空闲连接数，避免频繁创建和销毁连接带来的开销
	sqlDB.SetMaxIdleConns(20)
	// 设置连接的最大复用时间，防止复用被数据库主动断开的“死连接”
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 自动迁移表结构 (仅限开发阶段使用，生产环境应使用 SQL 脚本)
	if err := db.AutoMigrate(&model.ShortLink{}); err != nil {
		log.Printf("警告：自动迁移表结构失败: %v", err)
	}

	// ==========================================
	// 2. 初始化 Redis (go-redis)
	// ==========================================
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
		DB:       0, // 使用默认 DB
		// 【面试必考：Redis 连接池调优】
		PoolSize:     100, // 连接池大小
		MinIdleConns: 10,  // 最小空闲连接数
	})

	// 测试 Redis 连接是否正常
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	return &Storage{
		DB:    db,
		Redis: rdb,
	}, nil
}
