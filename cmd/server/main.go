package main

import (
	"fmt"
	"log"
	"shortlink-system/internal/api"
	"shortlink-system/internal/middleware"
	"shortlink-system/internal/repository"
	"shortlink-system/internal/service"
	"shortlink-system/pkg/config"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 加载配置
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 初始化存储层
	storageCfg := repository.Config{
		MySQLDSN:  cfg.Database.MySQL.DSN,
		RedisAddr: cfg.Database.Redis.Addr,
		RedisPass: cfg.Database.Redis.Pass,
	}
	repo, err := repository.NewStorage(storageCfg)
	if err != nil {
		log.Fatalf("初始化存储层失败: %v", err)
	}

	// 3. 初始化 Service
	svc, err := service.NewShortLinkService(cfg.App.URLPrefix, repo)
	if err != nil {
		log.Fatalf("初始化 Service 失败: %v", err)
	}

	// 4. 初始化 API 处理器
	handler := api.NewHandler(svc)

	// 5. 配置 Gin 路由
	gin.SetMode(cfg.Server.Mode)
	router := gin.New()
	router.Use(gin.Recovery())

	// ==========================================
	// 【核心装配】注入分布式限流中间件
	// 策略：桶容量为 10，每秒产生 2 个令牌
	// 意味着：允许瞬间爆发 10 个请求，但长期均速被限制在 2 QPS
	// ==========================================
	limiter := middleware.RateLimitMiddleware(repo.Redis, 10, 2)

	// 对生成短链接口应用限流器
	// 将原来的 handler.RegisterRoutes(router) 拆解，以便精细化控制
	apiV1 := router.Group("/api/v1")
	{
		// 只有发请求制造短链的才限流
		apiV1.POST("/shorten", limiter, handler.GenerateShortLink)
	}

	// 跳转接口由于有布隆过滤器和 Redis 缓存保护，且读流量极大，可以选择不限流，或使用更高的阈值
	router.GET("/:code", handler.Redirect)

	// 6. 启动 HTTP 服务
	log.Printf("短链接系统启动成功！监听端口 :%d", cfg.Server.Port)
	if err := router.Run(fmt.Sprintf(":%d", cfg.Server.Port)); err != nil {
		log.Fatalf("服务器异常退出: %v", err)
	}
}
