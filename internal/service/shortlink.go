package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"shortlink-system/internal/model"
	"shortlink-system/internal/repository"
	"shortlink-system/pkg/base62"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/snowflake"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ShortLinkService 聚合核心分布式组件和底层存储
type ShortLinkService struct {
	node      *snowflake.Node
	bloom     *bloom.RedisBloomFilter // Redis 分布式版本
	b62       *base62.Base62          // 实例化对象，挂载自定义乱序字典
	repo      *repository.Storage
	urlPrefix string
}

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
	redisBloom := bloom.NewRedisBloom(repo.Redis, "sys:bloom:shortcodes", 1_000_000, 5)

	return &ShortLinkService{
		node:      node,
		bloom:     redisBloom,
		b62:       base62.NewBase62(), // 采用默认的防遍历乱序字典
		repo:      repo,
		urlPrefix: prefix,
	}, nil
}

// Create 生成短链：雪花ID -> Base62 -> MySQL -> Redis(带抖动) -> 分布式 Bloom
func (s *ShortLinkService) Create(ctx context.Context, longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("URL 不能为空")
	}

	// 1. 生成分布式唯一 ID
	id, err := s.node.Generate()
	if err != nil {
		return "", fmt.Errorf("生成 ID 失败 (系统可能正遭遇严重时钟回拨): %w", err)
	}

	// 2. 零内存分配的高效编码
	shortCode := s.b62.Encode(uint64(id))

	linkRecord := model.ShortLink{
		ID:        uint64(id),
		ShortCode: shortCode,
		LongURL:   longURL,
	}

	// 3. 落盘到 MySQL (持久化)
	if err := s.repo.DB.WithContext(ctx).Create(&linkRecord).Error; err != nil {
		return "", fmt.Errorf("数据库写入失败: %w", err)
	}

	// 4. 写入 Redis 缓存 (加入随机抖动过期时间，防止同一批生成的冷数据在同一时刻大面积失效引发雪崩)
	expiration := s.generateJitterExpiration()
	err = s.repo.Redis.Set(ctx, "shortlink:"+shortCode, longURL, expiration).Err()
	if err != nil {
		// 缓存写入失败不应该阻塞主流程，记录错误即可
		fmt.Printf("警告: Redis 缓存写入失败: %v\n", err)
	}

	// 5. 极速更新分布式布隆过滤器 (通过 Pipeline 批量执行)
	if err := s.bloom.Add(ctx, shortCode); err != nil {
		fmt.Printf("警告: 布隆过滤器更新失败: %v\n", err)
	}

	return s.urlPrefix + shortCode, nil
}

// Redirect 短链跳转：分布式 Bloom 拦截 -> Redis 缓存 -> MySQL 回源 -> 回写缓存
func (s *ShortLinkService) Redirect(ctx context.Context, shortCode string) (string, error) {
	// 1. 第一道防线：Redis 分布式布隆过滤器拦截 (绝对防御缓存穿透)
	mightExist, err := s.bloom.Contains(ctx, shortCode)
	if err != nil {
		// 高可用降级策略：如果 Redis 管道查询失败，宁可放行到下一步查数据库，也不能直接阻断正常用户的访问
		fmt.Printf("布隆过滤器查询异常，触发降级放行: %v\n", err)
	} else if !mightExist {
		return "", errors.New("请求非法：该短链绝对不存在")
	}

	cacheKey := "shortlink:" + shortCode

	// 2. 第二道防线：查 Redis 缓存 (承担 99% 的读流量)
	longURL, err := s.repo.Redis.Get(ctx, cacheKey).Result()
	if err == nil {
		return longURL, nil // Cache Hit，直接起飞
	} else if err != redis.Nil {
		fmt.Printf("Redis 查询异常: %v\n", err)
	}

	// 3. 第三道防线：Cache Miss，去 MySQL 回源
	var linkRecord model.ShortLink
	err = s.repo.DB.WithContext(ctx).Where("short_code = ?", shortCode).First(&linkRecord).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 这里处理的是布隆过滤器的“假阳性”
			return "", errors.New("短链已失效或不存在")
		}
		return "", fmt.Errorf("数据库查询失败: %w", err)
	}

	// 4. Cache Aside 核心逻辑：回写缓存，并同样加入随机抖动防止雪崩
	s.repo.Redis.Set(ctx, cacheKey, linkRecord.LongURL, s.generateJitterExpiration())

	return linkRecord.LongURL, nil
}

// generateJitterExpiration 生成带有随机抖动的过期时间 (基础 24 小时 ± 0~60分钟抖动)
func (s *ShortLinkService) generateJitterExpiration() time.Duration {
	base := 24 * time.Hour
	// 随机增加 0 ~ 3600 秒的抖动，打散缓存失效时间点
	jitter := time.Duration(rand.Intn(3600)) * time.Second
	return base + jitter
}
