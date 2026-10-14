package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeDeps 用于模拟主库/Redis 的各种故障组合。
type fakeDeps struct {
	masterErr error
	redisErr  error
}

func (f fakeDeps) PingMaster(context.Context) error { return f.masterErr }
func (f fakeDeps) PingRedis(context.Context) error  { return f.redisErr }

func newHealthRouter(deps DependencyPinger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHealthHandler(deps).RegisterRoutes(router)
	// 模拟生产路由结构：静态探针路由与短码通配路由同级共存。
	router.GET("/:code", func(c *gin.Context) { c.String(http.StatusNotFound, "code="+c.Param("code")) })
	return router
}

func doGet(t *testing.T, router *gin.Engine, path string) (int, map[string]any, string) {
	t.Helper()
	code, raw := doGetRaw(t, router, path)

	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, raw)
	}
	return code, body, raw
}

// doGetRaw 用于断言非 JSON 响应（如短码通配路由返回的纯文本）。
func doGetRaw(t *testing.T, router *gin.Engine, path string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

// 存活探针不应受下游故障影响，否则 Redis 抖动会让 kubelet 重启所有 Pod。
func TestLiveIgnoresDependencyFailure(t *testing.T) {
	router := newHealthRouter(fakeDeps{
		masterErr: errors.New("mysql down"),
		redisErr:  errors.New("redis down"),
	})

	code, body, _ := doGet(t, router, "/health")
	if code != http.StatusOK {
		t.Fatalf("存活探针应始终返回 200: got=%d", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("存活探针状态错误: %v", body["status"])
	}
}

// 探针路由不能被 /:code 通配路由吞掉，否则 K8s 探针会拿到 404。
func TestHealthRoutesNotShadowedByWildcard(t *testing.T) {
	router := newHealthRouter(fakeDeps{})

	if code, _, _ := doGet(t, router, "/health"); code != http.StatusOK {
		t.Fatalf("/health 被通配路由捕获: got=%d", code)
	}
	if code, _, _ := doGet(t, router, "/ready"); code != http.StatusOK {
		t.Fatalf("/ready 被通配路由捕获: got=%d", code)
	}

	// 反向确认通配路由依然工作。
	code, raw := doGetRaw(t, router, "/abc123")
	if code != http.StatusNotFound || raw != "code=abc123" {
		t.Fatalf("短码通配路由异常: code=%d body=%s", code, raw)
	}
}

func TestReadyAllDependenciesHealthy(t *testing.T) {
	router := newHealthRouter(fakeDeps{})

	code, body, _ := doGet(t, router, "/ready")
	if code != http.StatusOK {
		t.Fatalf("依赖正常时应返回 200: got=%d", code)
	}
	if body["status"] != "ready" {
		t.Fatalf("状态应为 ready: %v", body["status"])
	}
	checks := body["checks"].(map[string]any)
	if checks["mysql_master"] != "ok" || checks["redis"] != "ok" {
		t.Fatalf("依赖明细错误: %v", checks)
	}
}

// 主库是事实源，不可用时必须摘除流量。
func TestReadyFailsWhenMasterDown(t *testing.T) {
	router := newHealthRouter(fakeDeps{masterErr: errors.New("dial tcp timeout")})

	code, body, _ := doGet(t, router, "/ready")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("主库故障应返回 503: got=%d", code)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("状态应为 not_ready: %v", body["status"])
	}
}

// Redis 故障已被业务链路降级承接，不能因此摘除全部实例，否则降级会变成完全不可用。
func TestReadyDegradesButStaysReadyWhenOnlyRedisDown(t *testing.T) {
	router := newHealthRouter(fakeDeps{redisErr: errors.New("redis connection refused")})

	code, body, _ := doGet(t, router, "/ready")
	if code != http.StatusOK {
		t.Fatalf("仅 Redis 故障不应摘除流量: got=%d", code)
	}
	if body["status"] != "degraded" {
		t.Fatalf("状态应为 degraded: %v", body["status"])
	}
	checks := body["checks"].(map[string]any)
	if checks["mysql_master"] != "ok" {
		t.Fatalf("主库状态应为 ok: %v", checks["mysql_master"])
	}
	if checks["redis"] == "ok" {
		t.Fatalf("Redis 故障未上报: %v", checks["redis"])
	}
}
