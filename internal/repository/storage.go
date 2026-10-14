package repository

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"shortlink-system/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Storage struct {
	MasterDB    *gorm.DB
	SlaveDBs    []*gorm.DB
	RedisClient redis.Cmdable
	readIndex   int32
}

type Config struct {
	MySQLMasterDSN string
	MySQLSlaveDSNs []string
	RedisAddrs     []string
	RedisPass      string
}

func NewStorage(cfg Config) (*Storage, error) {
	masterDB, err := initDB(cfg.MySQLMasterDSN, true)
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 主库失败: %w", err)
	}

	var slaveDBs []*gorm.DB
	for _, dsn := range cfg.MySQLSlaveDSNs {
		slaveDB, err := initDB(dsn, false)
		if err != nil {
			log.Printf("警告: 连接 MySQL 从库失败 [%s]: %v", dsn, err)
			continue
		}
		slaveDBs = append(slaveDBs, slaveDB)
	}

	var redisClient redis.Cmdable
	if len(cfg.RedisAddrs) > 1 {
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:           cfg.RedisAddrs,
			Password:        cfg.RedisPass,
			PoolSize:        500,
			MinIdleConns:    50,
			MaxIdleConns:    100,
			ConnMaxIdleTime: 5 * time.Minute,
			ConnMaxLifetime: 30 * time.Minute,
			DialTimeout:     5 * time.Second,
			ReadTimeout:     3 * time.Second,
			WriteTimeout:    3 * time.Second,
			ReadOnly:        false,
			RouteByLatency:  true,
		})
	} else if len(cfg.RedisAddrs) == 1 {
		redisClient = redis.NewClient(&redis.Options{
			Addr:            cfg.RedisAddrs[0],
			Password:        cfg.RedisPass,
			DB:              0,
			PoolSize:        500,
			MinIdleConns:    50,
			MaxIdleConns:    100,
			ConnMaxIdleTime: 5 * time.Minute,
			ConnMaxLifetime: 30 * time.Minute,
			DialTimeout:     5 * time.Second,
			ReadTimeout:     3 * time.Second,
			WriteTimeout:    3 * time.Second,
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
		MasterDB:    masterDB,
		SlaveDBs:    slaveDBs,
		RedisClient: redisClient,
	}, nil
}

func (s *Storage) FindByLongURLHash(ctx context.Context, urlHash string) (*model.ShortLink, error) {
	var link model.ShortLink
	err := s.GetReadDB(ctx).Where("long_url_hash = ?", urlHash).Take(&link).Error
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// FindByLongURLHashFromMaster 从 MySQL 主库查询 long_url_hash 对应的短链记录。
// 用于 Create 流程中的去重查询，避免主从延迟导致读取到旧数据。
func (s *Storage) FindByLongURLHashFromMaster(ctx context.Context, urlHash string) (*model.ShortLink, error) {
	var link model.ShortLink
	err := s.GetWriteDB(ctx).Select("short_code").Where("long_url_hash = ?", urlHash).Take(&link).Error
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// ListShortCodesAfter 按主键增量读取短码，用于后台重建 Bloom 过滤器。
func (s *Storage) ListShortCodesAfter(ctx context.Context, lastID uint64, limit int) ([]model.ShortLink, error) {
	var links []model.ShortLink
	err := s.MasterDB.WithContext(ctx).
		Select("id", "short_code").
		Where("id > ?", lastID).
		Order("id ASC").
		Limit(limit).
		Find(&links).Error
	return links, err
}

func (s *Storage) RedisGetURLHash(ctx context.Context, urlHash string) (string, error) {
	return s.RedisClient.Get(ctx, "urlhash:"+urlHash).Result()
}

func (s *Storage) RedisSetURLHash(ctx context.Context, urlHash, shortCode string, ttl time.Duration) error {
	return s.RedisClient.Set(ctx, "urlhash:"+urlHash, shortCode, ttl).Err()
}

func (s *Storage) GetReadDB(ctx context.Context) *gorm.DB {
	if len(s.SlaveDBs) == 0 {
		return s.MasterDB.WithContext(ctx)
	}

	idx := atomic.AddInt32(&s.readIndex, 1) % int32(len(s.SlaveDBs))
	return s.SlaveDBs[idx].WithContext(ctx)
}

func (s *Storage) GetWriteDB(ctx context.Context) *gorm.DB {
	return s.MasterDB.WithContext(ctx)
}

// PingMaster 检查 MySQL 主库连通性，供就绪探针判定服务能否接收写流量。
// 注意：这里只探主库，不走从库。从库挂了读路径仍可由主库兜底，
// 但主库不可用意味着短链无法创建（事实源缺失），属于真正的不就绪。
func (s *Storage) PingMaster(ctx context.Context) error {
	sqlDB, err := s.MasterDB.DB()
	if err != nil {
		return fmt.Errorf("获取主库连接池失败: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// PingRedis 检查 Redis 连通性，供就绪探针上报降级状态。
// Redis 故障已被业务链路降级承接（读回源 MySQL、限流切本地），
// 因此探针只上报 degraded，不据此摘除流量。
func (s *Storage) PingRedis(ctx context.Context) error {
	return s.RedisClient.Ping(ctx).Err()
}

func (s *Storage) RedisSetWithHashTag(ctx context.Context, shortCode string, longURL string, ttl time.Duration) error {
	key := s.GetRedisKeyWithHashTag(shortCode)
	return s.RedisClient.Set(ctx, key, longURL, ttl).Err()
}

func (s *Storage) RedisGetWithHashTag(ctx context.Context, shortCode string) (string, error) {
	key := s.GetRedisKeyWithHashTag(shortCode)
	return s.RedisClient.Get(ctx, key).Result()
}

func (s *Storage) GetRedisKeyWithHashTag(shortCode string) string {
	return fmt.Sprintf("shortlink:{%s}", shortCode)
}

func (s *Storage) GetBloomKeyWithHashTag(shortCode string) string {
	return "sys:bloom:{shortcodes}"
}

func (s *Storage) BatchRedisSet(ctx context.Context, pairs map[string]string, ttl time.Duration) error {
	if len(pairs) == 0 {
		return nil
	}

	pipe := s.RedisClient.Pipeline()
	for key, value := range pairs {
		pipe.Set(ctx, key, value, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Storage) BatchRedisGet(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	pipe := s.RedisClient.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	results := make(map[string]string, len(keys))
	for i, key := range keys {
		if val, err := cmds[i].Result(); err == nil {
			results[key] = val
		}
	}
	return results, nil
}

func ExtractHashTagKey(key string) string {
	if strings.HasPrefix(key, "{") {
		if idx := strings.Index(key, "}"); idx != -1 {
			return key[1:idx]
		}
	}
	return key
}

func initDB(dsn string, migrate bool) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Silent),
		PrepareStmt: true,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(200)
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetConnMaxLifetime(2 * time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	if migrate {
		if err := db.AutoMigrate(&model.ShortLink{}, &model.OutboxMessage{}); err != nil {
			log.Printf("警告：自动迁移表结构失败: %v", err)
		}
	}

	return db, nil
}
