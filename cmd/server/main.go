package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"shortlink-system/internal/api"
	"shortlink-system/internal/metrics"
	"shortlink-system/internal/middleware"
	"shortlink-system/internal/repository"
	"shortlink-system/internal/service"
	"shortlink-system/pkg/config"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	// 【监控体系】必须放在路由注册的最前面
	// ==========================================
	// 全局 HTTP 耗时与 QPS 记录中间件
	router.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next() // 先执行后续逻辑

		path := c.FullPath()
		// 对于 404 等没有匹配到路由的情况，FullPath 为空
		if path == "" {
			path = "unknown"
		}
		method := c.Request.Method
		status := strconv.Itoa(c.Writer.Status())

		// 记录请求总数和耗时
		metrics.HttpRequestsTotal.WithLabelValues(path, method, status).Inc()
		metrics.HttpRequestDuration.WithLabelValues(path, method).Observe(time.Since(start).Seconds())
	})

	// 暴露 Prometheus 抓取接口 (现在它自己的耗时也能被监控到了)
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// ==========================================
	// 【核心装配】注入分布式限流中间件
	// ==========================================
	limiter := middleware.RateLimitMiddleware(repo.Redis, 10, 2)

	// 业务路由注册
	apiV1 := router.Group("/api/v1")
	{
		// 只有发请求制造短链的才限流
		apiV1.POST("/shorten", limiter, handler.GenerateShortLink)
	}

	// 跳转接口 (由布隆过滤器保护，不强制走频率限流)
	router.GET("/:code", handler.Redirect)

	// ==========================================
	// 【优雅停机】SSP 级微服务标准启动流程
	// ==========================================
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	// 在独立的 Goroutine 中启动服务，不阻塞主线程
	go func() {
		log.Printf("短链接系统启动成功！监听端口 :%d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器监听异常: %v", err)
		}
	}()

	// 等待操作系统的中断信号
	quit := make(chan os.Signal, 1)
	// kill 默认发送 syscall.SIGTERM
	// kill -2 发送 syscall.SIGINT (相当于 Ctrl+C)
	// kill -9 发送 syscall.SIGKILL (不能被捕获，直接强杀)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 阻塞在此，直到收到退出信号
	<-quit
	log.Println("接收到停止信号，准备优雅关闭服务器...")

	// 预留 5 秒钟的超时时间，给正在处理中的请求一个收尾的机会
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("服务器强制关闭: %v", err)
	}

	log.Println("连接已释放，服务器安全退出。")
}
