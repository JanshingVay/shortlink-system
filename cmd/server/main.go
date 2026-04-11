package main

import (
	"log"
	"shortlink-system/internal/api"
	"shortlink-system/internal/repository"
	"shortlink-system/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := repository.Config{
		MySQLDSN:  "root:123456@tcp(127.0.0.1:3306)/shortlink?charset=utf8mb4&parseTime=True&loc=Local",
		RedisAddr: "127.0.0.1:6379",
		RedisPass: "",
	}
	repo, err := repository.NewStorage(cfg)
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
