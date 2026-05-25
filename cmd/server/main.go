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
	"shortlink-system/pkg/config"

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

	handler := api.NewHandler(svc)

	gin.SetMode(cfg.Server.Mode)
	router := gin.New()
	router.Use(gin.Recovery())

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

	log.Println("服务已安全关闭")
}
