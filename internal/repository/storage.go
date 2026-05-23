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

type Storage struct {
	DB          *gorm.DB
	SlaveDBs    []*gorm.DB
	RedisClient redis.Cmdable
}

type Config struct {
	MySQLMasterDSN string
	MySQLSlaveDSNs []string
	RedisAddrs     []string
	RedisPass      string
}

func NewStorage(cfg Config) (*Storage, error) {
	masterDB, err := initDB(cfg.MySQLMasterDSN)
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 主库失败: %w", err)
	}

	var slaveDBs []*gorm.DB
	for _, dsn := range cfg.MySQLSlaveDSNs {
		slaveDB, err := initDB(dsn)
		if err != nil {
			log.Printf("警告: 连接 MySQL 从库失败 [%s]: %v", dsn, err)
			continue
		}
		slaveDBs = append(slaveDBs, slaveDB)
	}

	var redisClient redis.Cmdable
	if len(cfg.RedisAddrs) > 1 {
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.RedisAddrs,
			Password:     cfg.RedisPass,
			PoolSize:     100,
			MinIdleConns: 10,
		})
	} else if len(cfg.RedisAddrs) == 1 {
		redisClient = redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddrs[0],
			Password:     cfg.RedisPass,
			DB:           0,
			PoolSize:     100,
			MinIdleConns: 10,
		})
	} else {
		return nil, fmt.Errorf("未配置 Redis 地址")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	return &Storage{
		DB:          masterDB,
		SlaveDBs:    slaveDBs,
		RedisClient: redisClient,
	}, nil
}

func initDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := db.AutoMigrate(&model.ShortLink{}); err != nil {
		log.Printf("警告：自动迁移表结构失败: %v", err)
	}

	return db, nil
}
