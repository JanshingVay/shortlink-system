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

type ShortLinkService struct {
	node      *snowflake.Node
	bloom     *bloom.RedisBloomFilter // 替换为 Redis 版本
	b62       *base62.Base62          // 替换为实例化版本（携带自定义字典）
	repo      *repository.Storage
	urlPrefix string
}

// NewShortLinkService 初始化服务
func NewShortLinkService(prefix string, repo *repository.Storage) (*ShortLinkService, error) {
	// 1. 动态获取 K8s Pod IP 计算 Worker ID，彻底告别硬编码
	nodeID := snowflake.FetchWorkerIDByIP()
	node, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("初始化雪花算法节点失败: %w", err)
	}

	// 2. 初始化 Redis 分布式布隆过滤器
	redisBloom := bloom.NewRedisBloom(repo.Redis, "sys:bloom:shortcodes", 1_000_000, 5)

	return &ShortLinkService{
		node:      node,
		bloom:     redisBloom,
		b62:       base62.NewBase62(), // 采用防遍历乱序字典
		repo:      repo,
		urlPrefix: prefix,
	}, nil
}

// Create 生成短链
func (s *ShortLinkService) Create(ctx context.Context, longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("URL 不能为空")
	}

	id, err := s.node.Generate()
	if err != nil {
		return "", fmt.Errorf("生成 ID 失败 (可能遭遇严重时钟回拨): %w", err)
	}

	shortCode := s.b62.Encode(uint64(id))

	linkRecord := model.ShortLink{
		ID:        uint64(id),
		ShortCode: shortCode,
		LongURL:   longURL,
	}

	// 1. 落盘到 MySQL
	if err := s.repo.DB.WithContext(ctx).Create(&linkRecord).Error; err != nil {
		return "", fmt.Errorf("数据库写入失败: %w", err)
	}

	// 2. 写入 Redis 缓存 (加入随机抖动，防止批量生成的短链在同一时刻失效引发雪崩)
	expiration := s.generateJitterExpiration()
	err = s.repo.Redis.Set(ctx, "shortlink:"+shortCode, longURL, expiration).Err()
	if err != nil {
		fmt.Printf("警告: Redis 缓存写入失败: %v\n", err) // 生产环境应接入 Zap 或 Logrus
	}

	// 3. 写入分布式布隆过滤器 (需要传入 ctx)
	if err := s.bloom.Add(ctx, shortCode); err != nil {
		fmt.Printf("警告: 布隆过滤器更新失败: %v\n", err)
	}

	return s.urlPrefix + shortCode, nil
}

// Redirect 短链跳转
func (s *ShortLinkService) Redirect(ctx context.Context, shortCode string) (string, error) {
	// 1. 第一道防线：Redis 分布式布隆过滤器拦截
	mightExist, err := s.bloom.Contains(ctx, shortCode)
	if err != nil {
		// 如果 Redis 管道查询失败，出于高可用原则，降级放行到下一步，而不是直接报错
		fmt.Printf("布隆过滤器查询异常，触发降级: %v\n", err)
	} else if !mightExist {
		return "", errors.New("请求非法：该短链绝对不存在")
	}

	cacheKey := "shortlink:" + shortCode

	// 2. 第二道防线：查 Redis 缓存
	longURL, err := s.repo.Redis.Get(ctx, cacheKey).Result()
	if err == nil {
		return longURL, nil // Cache Hit
	} else if err != redis.Nil {
		fmt.Printf("Redis 查询异常: %v\n", err)
	}

	// 3. 第三道防线：查 MySQL
	var linkRecord model.ShortLink
	err = s.repo.DB.WithContext(ctx).Where("short_code = ?", shortCode).First(&linkRecord).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("短链已失效或不存在")
		}
		return "", fmt.Errorf("数据库查询失败: %w", err)
	}

	// 4. Cache Aside 回写缓存，同样加入随机抖动
	s.repo.Redis.Set(ctx, cacheKey, linkRecord.LongURL, s.generateJitterExpiration())

	return linkRecord.LongURL, nil
}

// generateJitterExpiration 生成带有随机抖动的过期时间 (24小时 ± 0~60分钟)
func (s *ShortLinkService) generateJitterExpiration() time.Duration {
	base := 24 * time.Hour
	// 随机增加 0 ~ 3600 秒的抖动
	jitter := time.Duration(rand.Intn(3600)) * time.Second
	return base + jitter
}
