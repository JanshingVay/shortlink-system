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
	cacheTTLBase   = 24 * time.Hour
	cacheTTLJitter = 3600 * time.Second
	nullCacheTTL   = 5 * time.Minute
)

// CacheSyncConsumer 缓存同步消费者
type CacheSyncConsumer struct {
	repo         *repository.Storage
	l1Cache      *cache.L1Cache
	urlHashCache *cache.L1Cache
	bloom        *bloom.RedisBloomFilter
}

// NewCacheSyncConsumer 创建缓存同步消费者
func NewCacheSyncConsumer(repo *repository.Storage, l1Cache, urlHashCache *cache.L1Cache, bloom *bloom.RedisBloomFilter) *CacheSyncConsumer {
	return &CacheSyncConsumer{
		repo:         repo,
		l1Cache:      l1Cache,
		urlHashCache: urlHashCache,
		bloom:        bloom,
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

	// 正向缓存、反向索引和 Bloom 置位合并到一个 Pipeline。
	if err := c.withRetry(ctx, 3, func(ctx context.Context) error {
		pipe := c.repo.RedisClient.Pipeline()
		pipe.Set(ctx, shortCodeKey, msg.LongURL, expiration)
		pipe.Set(ctx, "urlhash:"+msg.URLHash, msg.ShortCode, expiration)
		c.bloom.QueueAdd(ctx, pipe, msg.ShortCode)
		_, err := pipe.Exec(ctx)
		return err
	}); err != nil {
		log.Printf("[CacheSyncConsumer] 批量同步 Redis 缓存失败 [short_code=%s]: %v", msg.ShortCode, err)
		return err
	}

	// Redis 成功后再更新两个本地缓存。
	c.l1Cache.Set(shortCodeKey, msg.LongURL)
	if c.urlHashCache != nil {
		c.urlHashCache.Set("urlhash:"+msg.URLHash, msg.ShortCode)
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
