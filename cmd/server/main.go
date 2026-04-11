package main

import (
	"log"
	"shortlink-system/internal/api"
	"shortlink-system/internal/repository"
	"shortlink-system/internal/service"
	"shortlink-system/pkg/config"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 加载配置
	cfg, err := config.LoadConfig("config.yaml")
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

	// 2. 初始化 Service 层，把 repo 注入进去
	svc, err := service.NewShortLinkService(1, "http://localhost:8080/", repo)
	if err != nil {
		log.Fatalf("初始化 Service 失败: %v", err)
	}

	// 2. 初始化 API 处理器
	handler := api.NewHandler(svc)

	// 3. 配置 Gin 路由框架
	// 面试小技巧：生产环境建议用 gin.New() 并自行挂载 Recovery 和 Logger 中间件
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	err = router.SetTrustedProxies(nil)

	// 4. 注册路由
	handler.RegisterRoutes(router)

	// 5. 启动 HTTP 服务
	log.Println("短链接系统启动成功！监听端口 :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("服务器异常退出: %v", err)
	}
}
