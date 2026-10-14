package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// readyCheckTimeout 就绪探针单次检查的下游超时。
// 必须显著小于 kubelet 的 timeoutSeconds，否则探针会因超时而非真实的依赖故障失败。
const readyCheckTimeout = 2 * time.Second

// DependencyPinger 抽象就绪探针所需的最小依赖，便于用测试替身覆盖各种故障组合。
// *repository.Storage 已实现该接口。
type DependencyPinger interface {
	PingMaster(ctx context.Context) error
	PingRedis(ctx context.Context) error
}

// HealthHandler 提供存活与就绪两类探针。
type HealthHandler struct {
	deps DependencyPinger
}

// NewHealthHandler 创建健康检查处理器。
func NewHealthHandler(deps DependencyPinger) *HealthHandler {
	return &HealthHandler{deps: deps}
}

// RegisterRoutes 注册探针路由。
// 这两条是静态路由，与短码通配路由 /:code 同级，gin 会将静态路由优先匹配，
// 因此 "health"/"ready" 不会落进 Redirect 被当成短码处理。
func (h *HealthHandler) RegisterRoutes(router gin.IRouter) {
	router.GET("/health", h.Live)
	router.GET("/ready", h.Ready)
}

// Live 存活探针：只反映进程本身是否还能正常处理请求。
//
// 刻意不检查任何下游依赖：Redis 抖动或主从切换如果让存活探针失败，
// kubelet 会批量重启所有 Pod，把一次局部降级放大成整站不可用。
// 依赖可用性属于就绪（ready）的职责。
func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready 就绪探针：判定该实例是否应该被挂到 Service 后端接收流量。
//
// 判定策略：
//   - MySQL 主库不可用 -> 503 not_ready。主库是事实源，不可用则短链无法创建，
//     此时摘除流量比把 500 返回给客户端更好。
//   - 仅 Redis 不可用 -> 200 degraded。读路径可回源 MySQL、写路径可直查主库、
//     限流自动切本地，服务仍能对外提供有损但可用的能力；
//     若此时摘除全部实例，反而会把降级变成完全不可用。
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), readyCheckTimeout)
	defer cancel()

	checks := gin.H{}
	status := "ready"
	code := http.StatusOK

	if err := h.deps.PingMaster(ctx); err != nil {
		checks["mysql_master"] = err.Error()
		status = "not_ready"
		code = http.StatusServiceUnavailable
	} else {
		checks["mysql_master"] = "ok"
	}

	// Redis 状态无论主库是否可用都要采集，便于排障时一眼看到完整依赖视图。
	if err := h.deps.PingRedis(ctx); err != nil {
		checks["redis"] = err.Error()
		if code == http.StatusOK {
			status = "degraded"
		}
	} else {
		checks["redis"] = "ok"
	}

	c.JSON(code, gin.H{"status": status, "checks": checks})
}
