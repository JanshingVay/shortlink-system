package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shortlink-system/internal/model"
	"shortlink-system/internal/repository"
	"shortlink-system/pkg/base62"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/snowflake"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ShortLinkService 聚合了我们的核心组件和真实的底层存储
type ShortLinkService struct {
	node      *snowflake.Node
	bloom     *bloom.BloomFilter
	repo      *repository.Storage // 替换掉了原来的 mockDB
	urlPrefix string
}

// NewShortLinkService 初始化服务时，把封装好的 Storage 传进来
func NewShortLinkService(nodeID int64, prefix string, repo *repository.Storage) (*ShortLinkService, error) {
	node, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, err
	}

	return &ShortLinkService{
		node:      node,
		bloom:     bloom.New(1000000, 5),
		repo:      repo,
		urlPrefix: prefix,
	}, nil
}

// Create 生成短链：先写 MySQL，再写 Redis，最后加布隆
func (s *ShortLinkService) Create(ctx context.Context, longURL string) (string, error) {
	if longURL == "" {
		return "", errors.New("URL 不能为空")
	}

	// 1. 雪花算法生成 ID
	id, err := s.node.Generate()
	if err != nil {
		return "", err
	}

	// 2. Base62 编码
	shortCode := base62.Encode(uint64(id))

	// 3. 构造要插入数据库的 Model
	linkRecord := model.ShortLink{
		ID:        uint64(id),
		ShortCode: shortCode,
		LongURL:   longURL,
	}

	// 4. 落盘到 MySQL (持久化)
	if err := s.repo.DB.WithContext(ctx).Create(&linkRecord).Error; err != nil {
		return "", fmt.Errorf("数据库写入失败: %w", err)
	}

	// 5. 写入 Redis 缓存 (设置 24 小时过期时间，防止冷数据占满内存)
	// 面试考点：为什么要写 Redis？为了让刚生成的短链在被高频访问时，直接走内存。
	err = s.repo.Redis.Set(ctx, "shortlink:"+shortCode, longURL, 24*time.Hour).Err()
	if err != nil {
		// Redis 写入失败不应该阻塞主流程，打个日志即可 (这里为了演示简化处理)
		fmt.Printf("警告: Redis 缓存写入失败: %v\n", err)
	}

	// 6. 加入布隆过滤器 (防穿透装甲)
	s.bloom.Add(shortCode)

	return s.urlPrefix + shortCode, nil
}

// Redirect 短链跳转：布隆拦截 -> Redis 缓存 -> MySQL 兜底 -> 回写 Redis
func (s *ShortLinkService) Redirect(ctx context.Context, shortCode string) (string, error) {
	// 1. 第一道防线：布隆过滤器 (绝对防御缓存穿透)
	if !s.bloom.Contains(shortCode) {
		return "", errors.New("请求非法：该短链绝对不存在")
	}

	cacheKey := "shortlink:" + shortCode

	// 2. 第二道防线：查 Redis 缓存 (承担 99% 的读流量)
	longURL, err := s.repo.Redis.Get(ctx, cacheKey).Result()
	if err == nil {
		// 缓存命中 (Cache Hit)，直接起飞
		return longURL, nil
	} else if err != redis.Nil {
		// Redis 真的崩了或者网络超时，为了高可用，我们可以降级去查 MySQL，这里先记录错误
		fmt.Printf("Redis 查询异常: %v\n", err)
	}

	// 3. 第三道防线：Redis 没查到 (Cache Miss)，去查 MySQL
	var linkRecord model.ShortLink
	err = s.repo.DB.WithContext(ctx).Where("short_code = ?", shortCode).First(&linkRecord).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 布隆过滤器的“假阳性”：布隆说有，但数据库真没有。正常现象，返回 404
			return "", errors.New("短链已失效或不存在")
		}
		return "", fmt.Errorf("数据库查询失败: %w", err)
	}

	// 4. 关键步骤：查到了数据，必须回写到 Redis (Cache Aside 核心)
	// 面试考点：并且给过期时间加上一个“随机值”，防止大量缓存同时失效导致“缓存雪崩”！
	// 这里简单写 24 小时，实际生产会写 `24*time.Hour + 随机秒数`
	s.repo.Redis.Set(ctx, cacheKey, linkRecord.LongURL, 24*time.Hour)

	return linkRecord.LongURL, nil
}