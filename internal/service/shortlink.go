package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"shortlink-system/internal/model"
	"shortlink-system/internal/repository"
	"shortlink-system/pkg/base62"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/cache"
	"shortlink-system/pkg/snowflake"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

// ShortLinkService 聚合核心分布式组件和底层存储
type ShortLinkService struct {
	node       *snowflake.Node
	bloom      *bloom.RedisBloomFilter
	b62        *base62.Base62
	repo       *repository.Storage
	urlPrefix  string
	l1Cache    *cache.L1Cache
	urlHashL1  *cache.L1Cache
	redirects  singleflight.Group
	bloomReady atomic.Bool
	randMu     sync.Mutex
	randSrc    *rand.Rand
}

const (
	l1CacheCapacity = 100000
	l1CacheTTL      = 5 * time.Minute
	urlHashCapacity = 20000
	urlHashTTL      = 10 * time.Minute
	bloomBitSize    = 10_000_000 // 10 bits/element for the expected 1M short codes
	bloomHashCount  = 5
)

// NewShortLinkService 初始化服务
func NewShortLinkService(prefix string, repo *repository.Storage) (*ShortLinkService, error) {
	// 1. 动态获取 K8s Pod IP 计算 Worker ID，彻底解决容器漂移导致的 ID 冲突
	nodeID := snowflake.FetchWorkerIDByIP()
	node, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("初始化雪花算法节点失败 (WorkerID: %d): %w", nodeID, err)
	}

	// 2. 初始化 Redis 分布式布隆过滤器
	// 预估 100 万数据量，5 个哈希函数，Key 命名规范化
	// v2 避免新位图容量与旧位图的 offset 规则混用。
	redisBloom := bloom.NewRedisBloom(repo.RedisClient, "sys:bloom:shortcodes:v2", bloomBitSize, bloomHashCount)

	svc := &ShortLinkService{
		node:      node,
		bloom:     redisBloom,
		b62:       base62.NewBase62(),
		repo:      repo,
		urlPrefix: prefix,
		l1Cache:   cache.NewL1Cache(l1CacheCapacity, l1CacheTTL),
		urlHashL1: cache.NewL1Cache(urlHashCapacity, urlHashTTL),
		randSrc:   rand.New(rand.NewSource(time.Now().UnixNano() + int64(nodeID))),
	}
	return svc, nil
}

// Create 生成短链：MD5 去重 → 雪花ID → Base62 → MySQL + Outbox → 缓存写穿
// 核心设计：通过 MD5(long_url) 实现幂等去重，同一长链多次请求返回相同短链。
// 查重链路：Redis urlhash 缓存 → MySQL unique 索引兜底。
func (s *ShortLinkService) Create(ctx context.Context, longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("URL 不能为空")
	}

	urlHash := md5Hex(longURL)
	urlHashKey := "urlhash:" + urlHash

	// ==========================================
	// L1: 本地反向索引缓存，避免同一 Pod 重复 URL 每次都访问 Redis。
	// ==========================================
	if shortCode, ok := s.urlHashL1.Get(urlHashKey); ok && shortCode != "" {
		return s.urlPrefix + shortCode, nil
	}

	// ==========================================
	// L2: Redis 反向索引缓存 urlhash:{md5} → shortCode
	// ==========================================
	if shortCode, err := s.repo.RedisGetURLHash(ctx, urlHash); err == nil && shortCode != "" {
		s.urlHashL1.Set(urlHashKey, shortCode)
		return s.urlPrefix + shortCode, nil
	}

	// ==========================================
	// L2: MySQL 唯一索引兜底去重（走主库，避免主从延迟读到旧数据）
	// ==========================================
	existing, err := s.repo.FindByLongURLHashFromMaster(ctx, urlHash)
	if err == nil {
		// 已存在，回写 Redis 缓存
		cacheTTL := s.generateJitterExpiration()
		if setErr := s.repo.RedisSetURLHash(ctx, urlHash, existing.ShortCode, cacheTTL); setErr != nil {
			fmt.Printf("警告: 回写 urlhash 缓存失败: %v\n", setErr)
		}
		s.urlHashL1.Set(urlHashKey, existing.ShortCode)
		return s.urlPrefix + existing.ShortCode, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("查询去重失败: %w", err)
	}

	// ==========================================
	// 不存在：生成新短链
	// ==========================================
	id, err := s.node.Generate()
	if err != nil {
		return "", fmt.Errorf("生成 ID 失败 (系统可能正遭遇严重时钟回拨): %w", err)
	}

	shortCode := s.b62.Encode(uint64(id))

	linkRecord := model.ShortLink{
		ID:          uint64(id),
		ShortCode:   shortCode,
		LongURL:     longURL,
		LongURLHash: urlHash,
	}

	// 开启事务：同时写入业务表和 outbox 表，保证原子性
	tx := s.repo.GetWriteDB(ctx).Begin()
	if tx.Error != nil {
		return "", fmt.Errorf("开启事务失败: %w", tx.Error)
	}

	if err := tx.Create(&linkRecord).Error; err != nil {
		tx.Rollback()
		return "", fmt.Errorf("数据库写入失败: %w", err)
	}

	outboxMsg := &model.OutboxMessage{
		ShortCode: shortCode,
		LongURL:   longURL,
		URLHash:   urlHash,
		Status:    "pending",
	}
	if err := s.repo.CreateOutbox(tx, outboxMsg); err != nil {
		tx.Rollback()
		return "", fmt.Errorf("写入 outbox 失败: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return "", fmt.Errorf("事务提交失败: %w", err)
	}

	shortURL := s.urlPrefix + shortCode

	// 先更新本机 L1，保证当前 Pod 内立即可读；Redis/Bloom 写穿使用一个
	// Pipeline，降低原来 3 次串行网络往返的延迟。Outbox 仍作为最终一致性兜底。
	s.l1Cache.Set("shortlink:"+shortCode, longURL)
	s.urlHashL1.Set(urlHashKey, shortCode)

	// 写穿缓存是 best-effort；失败不影响主流程，Outbox 消费者会兜底同步。
	s.writeThrough(ctx, shortCode, longURL, urlHash)

	return shortURL, nil
}

// writeThrough 写穿缓存：将新短链同步写入 Redis 与 Bloom（Best-Effort，带指数退避重试）。
func (s *ShortLinkService) writeThrough(ctx context.Context, shortCode, longURL, urlHash string) {
	expiration := s.generateJitterExpiration()
	cacheKey := "shortlink:" + shortCode

	err := s.withRetry(ctx, 3, func(ctx context.Context) error {
		pipe := s.repo.RedisClient.Pipeline()
		pipe.Set(ctx, cacheKey, longURL, expiration)
		pipe.Set(ctx, "urlhash:"+urlHash, shortCode, expiration)
		s.bloom.QueueAdd(ctx, pipe, shortCode)
		_, err := pipe.Exec(ctx)
		return err
	})
	if err != nil {
		fmt.Printf("警告: 写穿缓存失败 [short_code=%s]: %v\n", shortCode, err)
	}
}

// L1Cache 返回本地 L1 缓存实例，供外部组件（如消费者）使用。
func (s *ShortLinkService) L1Cache() *cache.L1Cache {
	return s.l1Cache
}

// URLHashCache 返回本地 URL 反向索引缓存，供缓存同步消费者更新。
func (s *ShortLinkService) URLHashCache() *cache.L1Cache {
	return s.urlHashL1
}

// Bloom 返回布隆过滤器实例，供外部组件（如消费者）使用。
func (s *ShortLinkService) Bloom() *bloom.RedisBloomFilter {
	return s.bloom
}

// RebuildBloom 从主库增量重建当前版本的 Bloom。启动重建完成前，Redirect
// 会跳过 Bloom 并依赖 Redis/MySQL，保证扩容或版本切换期间不会误报 404。
func (s *ShortLinkService) RebuildBloom(ctx context.Context) error {
	const batchSize = 500
	var lastID uint64

	for {
		links, err := s.repo.ListShortCodesAfter(ctx, lastID, batchSize)
		if err != nil {
			return fmt.Errorf("读取短码重建 Bloom 失败: %w", err)
		}
		if len(links) == 0 {
			s.bloomReady.Store(true)
			log.Println("[Bloom] 重建完成")
			return nil
		}

		pipe := s.repo.RedisClient.Pipeline()
		for _, link := range links {
			s.bloom.QueueAdd(ctx, pipe, link.ShortCode)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("写入 Bloom 重建批次失败: %w", err)
		}
		lastID = links[len(links)-1].ID
	}
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// Redirect 短链跳转：L1 -> Redis -> Bloom 防穿透 -> MySQL -> 逐级回写。
// Redis 优先于 Bloom，避免每个 L1 冷请求多一次 Redis RTT。
func (s *ShortLinkService) Redirect(ctx context.Context, shortCode string) (string, error) {
	cacheKey := "shortlink:" + shortCode

	// ==========================================
	// L1: 本地内存缓存（0 网络开销，纳秒级）
	// ==========================================
	if value, ok := s.l1Cache.Get(cacheKey); ok {
		if value == "" {
			return "", errors.New("短链不存在")
		}
		return value, nil
	}

	value, err, _ := s.redirects.Do(shortCode, func() (any, error) {
		return s.resolveRedirect(ctx, shortCode, cacheKey)
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

// resolveRedirect 只在 L1 未命中时执行，并由 singleflight 合并同一短码的并发回源。
func (s *ShortLinkService) resolveRedirect(ctx context.Context, shortCode, cacheKey string) (string, error) {
	// Double-check：前一个并发请求可能已经在进入 singleflight 前填充了 L1。
	if value, ok := s.l1Cache.Get(cacheKey); ok {
		if value == "" {
			return "", errors.New("短链不存在")
		}
		return value, nil
	}

	// L2: Redis 命中是最常见的冷 L1 路径，必须优先于 Bloom。
	longURL, err := s.repo.RedisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		if longURL == "" {
			s.l1Cache.Set(cacheKey, "")
			return "", errors.New("短链不存在")
		}
		s.l1Cache.Set(cacheKey, longURL)
		return longURL, nil
	}
	if err != redis.Nil {
		fmt.Printf("Redis 查询异常: %v\n", err)
	}

	// 只有 Redis 未命中且 Bloom 已完成重建时才使用 Bloom。
	// 重建期间跳过 Bloom，避免新版本位图尚未覆盖旧数据而产生误报 404。
	if s.bloomReady.Load() {
		mightExist, bloomErr := s.bloom.Contains(ctx, shortCode)
		if bloomErr != nil {
			fmt.Printf("布隆过滤器查询异常，触发降级放行: %v\n", bloomErr)
		} else if !mightExist {
			s.l1Cache.Set(cacheKey, "")
			return "", errors.New("短链不存在")
		}
	}

	// L3: MySQL 从库回源，只读取重定向所需字段。
	var linkRecord model.ShortLink
	err = s.repo.GetReadDB(ctx).Select("long_url").Where("short_code = ?", shortCode).Take(&linkRecord).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 从库可能存在复制延迟。只有主库也确认不存在后，才允许写负缓存。
		err = s.repo.GetWriteDB(ctx).Select("long_url").Where("short_code = ?", shortCode).Take(&linkRecord).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if setErr := s.repo.RedisClient.Set(ctx, cacheKey, "", 5*time.Minute).Err(); setErr != nil {
				fmt.Printf("警告: 空值缓存写入失败: %v\n", setErr)
			}
			s.l1Cache.Set(cacheKey, "")
			return "", errors.New("短链不存在")
		}
		return "", fmt.Errorf("数据库查询失败: %w", err)
	}

	result := linkRecord.LongURL
	expiration := s.generateJitterExpiration()
	if err := s.repo.RedisClient.Set(ctx, cacheKey, result, expiration).Err(); err != nil {
		fmt.Printf("警告: Redis 缓存回写失败: %v\n", err)
	}
	s.l1Cache.Set(cacheKey, result)
	return result, nil
}

// generateJitterExpiration 生成带有随机抖动的过期时间 (基础 24 小时 ± 0~60分钟抖动)
func (s *ShortLinkService) generateJitterExpiration() time.Duration {
	base := 24 * time.Hour
	// 随机增加 0 ~ 3600 秒的抖动，打散缓存失效时间点
	s.randMu.Lock()
	jitter := time.Duration(s.randSrc.Intn(3600)) * time.Second
	s.randMu.Unlock()
	return base + jitter
}

// withRetry 带指数退避的重试执行器，用于提高缓存写操作的可靠性。
func (s *ShortLinkService) withRetry(ctx context.Context, maxAttempts int, fn func(context.Context) error) error {
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
