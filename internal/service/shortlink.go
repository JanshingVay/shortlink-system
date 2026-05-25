package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"shortlink-system/internal/model"
	"shortlink-system/internal/repository"
	"shortlink-system/pkg/base62"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/cache"
	"shortlink-system/pkg/snowflake"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ShortLinkService 聚合核心分布式组件和底层存储
type ShortLinkService struct {
	node      *snowflake.Node
	bloom     *bloom.RedisBloomFilter
	b62       *base62.Base62
	repo      *repository.Storage
	urlPrefix string
	l1Cache   *cache.L1Cache
	randMu    sync.Mutex
	randSrc   *rand.Rand
}

const (
	l1CacheCapacity = 100000
	l1CacheTTL      = 5 * time.Minute
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
	redisBloom := bloom.NewRedisBloom(repo.RedisClient, "sys:bloom:shortcodes", 1_000_000, 5)

	svc := &ShortLinkService{
		node:      node,
		bloom:     redisBloom,
		b62:       base62.NewBase62(),
		repo:      repo,
		urlPrefix: prefix,
		l1Cache:   cache.NewL1Cache(l1CacheCapacity, l1CacheTTL),
		randSrc:   rand.New(rand.NewSource(time.Now().UnixNano() + int64(nodeID))),
	}
	return svc, nil
}

// Create 生成短链：MD5 去重 → 雪花ID → Base62 → MySQL → 异步 Redis + 异步 Bloom
// 核心设计：通过 MD5(long_url) 实现幂等去重，同一长链多次请求返回相同短链。
// 查重链路：Redis urlhash 缓存 → MySQL unique 索引兜底。
func (s *ShortLinkService) Create(ctx context.Context, longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("URL 不能为空")
	}

	urlHash := md5Hex(longURL)

	// ==========================================
	// L1: Redis 反向索引缓存 urlhash:{md5} → shortCode
	// ==========================================
	if shortCode, err := s.repo.RedisGetURLHash(ctx, urlHash); err == nil && shortCode != "" {
		return s.urlPrefix + shortCode, nil
	}

	// ==========================================
	// L2: MySQL 唯一索引兜底去重
	// ==========================================
	existing, err := s.repo.FindByLongURLHash(ctx, urlHash)
	if err == nil {
		// 已存在，回写 Redis 缓存
		cacheTTL := s.generateJitterExpiration()
		if setErr := s.repo.RedisSetURLHash(context.Background(), urlHash, existing.ShortCode, cacheTTL); setErr != nil {
			fmt.Printf("警告: 回写 urlhash 缓存失败: %v\n", setErr)
		}
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

	if err := s.repo.GetWriteDB(ctx).Create(&linkRecord).Error; err != nil {
		return "", fmt.Errorf("数据库写入失败: %w", err)
	}

	shortURL := s.urlPrefix + shortCode

	expiration := s.generateJitterExpiration()

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := s.repo.RedisClient.Set(bgCtx, "shortlink:"+shortCode, longURL, expiration).Err(); err != nil {
			fmt.Printf("警告: Redis 缓存写入失败: %v\n", err)
			return
		}

		s.l1Cache.Set("shortlink:"+shortCode, longURL)

		if err := s.repo.RedisSetURLHash(bgCtx, urlHash, shortCode, expiration); err != nil {
			fmt.Printf("警告: urlhash 缓存写入失败: %v\n", err)
		}

		if err := s.bloom.Add(bgCtx, shortCode); err != nil {
			fmt.Printf("警告: 布隆过滤器更新失败: %v\n", err)
		}
	}()

	return shortURL, nil
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// Redirect 短链跳转：Bloom 防穿透 -> L1 内存 -> L2 Redis -> L3 MySQL -> 逐级回写
func (s *ShortLinkService) Redirect(ctx context.Context, shortCode string) (string, error) {
	cacheKey := "shortlink:" + shortCode

	// ==========================================
	// L1: 本地内存缓存（0 网络开销，纳秒级）
	// ==========================================
	if value, ok := s.l1Cache.Get(cacheKey); ok {
		return value, nil
	}

	// ==========================================
	// 防穿透层: Bloom 过滤器拦截不存在的 key
	// ==========================================
	mightExist, err := s.bloom.Contains(ctx, shortCode)
	if err != nil {
		fmt.Printf("布隆过滤器查询异常，触发降级放行: %v\n", err)
	} else if !mightExist {
		// 绝对不存在，空值缓存防止重复穿透
		s.l1Cache.Set(cacheKey, "")
		return "", errors.New("短链不存在")
	}

	// ==========================================
	// L2: Redis 集群（微秒级，承担主要流量）
	// ==========================================
	longURL, err := s.repo.RedisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		s.l1Cache.Set(cacheKey, longURL)
		return longURL, nil
	}
	if err != redis.Nil {
		fmt.Printf("Redis 查询异常: %v\n", err)
	}

	// ==========================================
	// L3: MySQL 从库回源（毫秒级，兜底保障）
	// ==========================================
	var linkRecord model.ShortLink
	err = s.repo.GetReadDB(ctx).Where("short_code = ?", shortCode).First(&linkRecord).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 空值缓存：5 分钟，防止攻击者重复穿透到 MySQL
			s.repo.RedisClient.Set(ctx, cacheKey+":null", "1", 5*time.Minute)
			return "", errors.New("短链不存在")
		}
		return "", fmt.Errorf("数据库查询失败: %w", err)
	}

	result := linkRecord.LongURL

	// 逐级回写：L3 -> L2 -> L1
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
