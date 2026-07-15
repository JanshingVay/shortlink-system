package worker

import (
	"context"
	"log"
	"time"

	"shortlink-system/internal/repository"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/cache"
	"shortlink-system/pkg/mq"
)

const (
	cacheTTLBase    = 24 * time.Hour
	cacheTTLJitter  = 3600 * time.Second
	nullCacheTTL    = 5 * time.Minute
)

// CacheSyncConsumer 缓存同步消费者
type CacheSyncConsumer struct {
	repo    *repository.Storage
	l1Cache *cache.L1Cache
	bloom   *bloom.RedisBloomFilter
}

// NewCacheSyncConsumer 创建缓存同步消费者
func NewCacheSyncConsumer(repo *repository.Storage, l1Cache *cache.L1Cache, bloom *bloom.RedisBloomFilter) *CacheSyncConsumer {
	return &CacheSyncConsumer{
		repo:    repo,
		l1Cache: l1Cache,
		bloom:   bloom,
	}
}

// Start 启动 RabbitMQ 消费者。
func (c *CacheSyncConsumer) Start(ctx context.Context, m *mq.RabbitMQ) error {
	log.Println("[CacheSyncConsumer] 启动")
	return m.Consume(ctx, c.handleMessage)
}

// handleMessage 处理单条缓存更新消息。
func (c *CacheSyncConsumer) handleMessage(msg mq.CacheUpdateMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	expiration := c.generateJitterExpiration()
	shortCodeKey := "shortlink:" + msg.ShortCode

	// 1. 写入 Redis 正向缓存 shortlink:{code} -> longURL
	if err := c.withRetry(ctx, 3, func(ctx context.Context) error {
		return c.repo.RedisClient.Set(ctx, shortCodeKey, msg.LongURL, expiration).Err()
	}); err != nil {
		log.Printf("[CacheSyncConsumer] 写入 Redis 正向缓存失败 [short_code=%s]: %v", msg.ShortCode, err)
		return err
	}

	// 2. 写入本地 L1 缓存
	c.l1Cache.Set(shortCodeKey, msg.LongURL)

	// 3. 写入 Redis 反向索引 urlhash:{md5} -> shortCode
	if err := c.withRetry(ctx, 3, func(ctx context.Context) error {
		return c.repo.RedisSetURLHash(ctx, msg.URLHash, msg.ShortCode, expiration)
	}); err != nil {
		log.Printf("[CacheSyncConsumer] 写入 urlhash 缓存失败 [short_code=%s]: %v", msg.ShortCode, err)
		return err
	}

	// 4. 更新布隆过滤器
	if err := c.withRetry(ctx, 3, func(ctx context.Context) error {
		return c.bloom.Add(ctx, msg.ShortCode)
	}); err != nil {
		log.Printf("[CacheSyncConsumer] 更新布隆过滤器失败 [short_code=%s]: %v", msg.ShortCode, err)
		return err
	}

	log.Printf("[CacheSyncConsumer] 缓存同步完成 [short_code=%s]", msg.ShortCode)
	return nil
}

// withRetry 带指数退避的重试执行器
func (c *CacheSyncConsumer) withRetry(ctx context.Context, maxAttempts int, fn func(context.Context) error) error {
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			select {
			case <-time.After(time.Duration(i*100) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := fn(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// generateJitterExpiration 生成带随机抖动的过期时间
func (c *CacheSyncConsumer) generateJitterExpiration() time.Duration {
	return cacheTTLBase
}
