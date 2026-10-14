package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shortlink-system/internal/api"
	"shortlink-system/internal/middleware"
	"shortlink-system/internal/repository"
	"shortlink-system/internal/service"
	"shortlink-system/internal/worker"
	"shortlink-system/pkg/config"
	"shortlink-system/pkg/mq"

	"github.com/gin-gonic/gin"
)

func main() {
	// 命令行参数：是否开启限流，默认开启
	enableRateLimit := flag.Bool("rate-limit", true, "是否开启限流 (默认开启，使用 --rate-limit=false 关闭)")
	flag.Parse()
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	storageCfg := repository.Config{
		MySQLMasterDSN: cfg.Database.MySQL.MasterDSN,
		MySQLSlaveDSNs: cfg.Database.MySQL.SlaveDSNs,
		RedisAddrs:     cfg.Database.Redis.ClusterAddrs,
		RedisPass:      cfg.Database.Redis.Pass,
	}
	repo, err := repository.NewStorage(storageCfg)
	if err != nil {
		log.Fatalf("初始化存储层失败: %v", err)
	}

	svc, err := service.NewShortLinkService(cfg.App.URLPrefix, repo)
	if err != nil {
		log.Fatalf("初始化 Service 失败: %v", err)
	}
	go func() {
		if err := svc.RebuildBloom(context.Background()); err != nil {
			log.Printf("[Bloom] 后台重建失败，继续使用 Redis/MySQL 回源: %v", err)
		}
	}()

	// 初始化 RabbitMQ
	rabbitMQURL := cfg.MQ.RabbitMQ.URL
	if rabbitMQURL == "" {
		rabbitMQURL = "amqp://guest:guest@localhost:5672/"
	}
	mqClient, err := mq.NewRabbitMQ(rabbitMQURL)
	if err != nil {
		log.Fatalf("初始化 RabbitMQ 失败: %v", err)
	}
	defer mqClient.Close()

	// 启动 Outbox Worker：扫描 outbox 表并推送消息到 RabbitMQ
	outboxWorker := worker.NewOutboxWorker(repo, mqClient)
	go outboxWorker.Start(context.Background())

	// 启动缓存同步消费者：从 RabbitMQ 消费消息并更新 Redis/Bloom
	cacheConsumer := worker.NewCacheSyncConsumer(repo, svc.L1Cache(), svc.URLHashCache(), svc.Bloom())
	go func() {
		if err := cacheConsumer.Start(context.Background(), mqClient); err != nil {
			log.Printf("缓存同步消费者退出: %v", err)
		}
	}()

	handler := api.NewHandler(svc)

	gin.SetMode(cfg.Server.Mode)
	router := gin.New()
	router.Use(gin.Recovery())

	// 健康检查路由必须在 /:code 通配路由之前注册。
	// 缺失这两个路由会导致 K8s 探针命中 /:code 并拿到 404，
	// 进而触发 Pod 反复重启且永远不 Ready。
	healthHandler := api.NewHealthHandler(repo)
	healthHandler.RegisterRoutes(router)

	if *enableRateLimit {
		limiter := middleware.RateLimitMiddleware(repo.RedisClient, 10, 2)
		apiV1 := router.Group("/api/v1")
		{
			apiV1.POST("/encode", limiter, handler.GenerateShortLink)
			apiV1.GET("/decode/:code", handler.Decode)
		}
		log.Println("[限流] 已开启，速率: 10容量 / 2req/s")
	} else {
		apiV1 := router.Group("/api/v1")
		{
			apiV1.POST("/encode", handler.GenerateShortLink)
			apiV1.GET("/decode/:code", handler.Decode)
		}
		log.Println("[限流] 已关闭")
	}

	router.GET("/:code", handler.Redirect)

	srv := &http.Server{
		Addr:           ":8080",
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		log.Printf("短链接系统启动成功！监听端口 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器异常退出: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在优雅关闭服务...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("服务关闭异常: %v", err)
	}

	outboxWorker.Stop()

	log.Println("服务已安全关闭")
}
