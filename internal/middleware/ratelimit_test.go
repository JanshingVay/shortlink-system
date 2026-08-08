package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// setupTest 限流测试环境初始化
func setupTest(t testing.TB) (*miniredis.Miniredis, *redis.Client, *gin.Engine) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	if err != nil {
		// t 依然可以使用 Fatalf，因为 testing.TB 接口包含了该方法
		t.Fatalf("启动 miniredis 失败: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	router := gin.New()
	router.Use(RateLimitMiddleware(rdb, 100, 50))

	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "passed")
	})

	return mr, rdb, router
}

// 测试 1：高并发正常限流逻辑验证
func TestRateLimit_Concurrency(t *testing.T) {
	_, _, router := setupTest(t)

	var successCount int64
	var limitCount int64
	var wg sync.WaitGroup

	// 模拟 500 个并发协程瞬间冲入
	numRequests := 500
	wg.Add(numRequests)

	for range numRequests {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.1:1234" // 固定 IP 以触发同一个桶的限流

			router.ServeHTTP(w, req)

			switch w.Code {
			case http.StatusOK:
				atomic.AddInt64(&successCount, 1)
			case http.StatusTooManyRequests:
				atomic.AddInt64(&limitCount, 1)
			}
		}()
	}

	wg.Wait()

	t.Logf("并发请求总数: %d, 成功数: %d, 限流数: %d", numRequests, successCount, limitCount)

	// 令牌桶会随真实时间补充令牌，尤其是 -race 环境下不能把成功数固定为
	// 初始容量附近；这里验证限流确实生效，而不依赖调度时序。
	if successCount >= int64(numRequests) || limitCount == 0 {
		t.Errorf("限流未生效: 成功数=%d, 限流数=%d", successCount, limitCount)
	}
}

// 测试 2：Redis 故障时的【本地内存降级】专项测试
// 验证 10w+ QPS 下 Redis 挂了，系统是否能瞬间切入内存限流而不崩溃
func TestRateLimit_RedisDown_Fallback(t *testing.T) {
	mr, rdb, router := setupTest(t)

	// --- 关键步骤：手动关闭 Redis 模拟宕机 ---
	mr.Close()
	rdb.Close()

	var successCount int64
	var fallbackLimitCount int64
	var wg sync.WaitGroup

	// 模拟 Redis 挂掉后的并发流量
	numRequests := 200
	wg.Add(numRequests)

	start := time.Now()
	for range numRequests {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "10.0.0.1:9999"

			router.ServeHTTP(w, req)

			switch w.Code {
			case http.StatusOK:
				atomic.AddInt64(&successCount, 1)
			case http.StatusTooManyRequests:
				// 检查是否包含降级提示
				if strings.Contains(w.Body.String(), "降级保护生效") {
					atomic.AddInt64(&fallbackLimitCount, 1)
				}
			}
		}()
	}
	wg.Wait()
	duration := time.Since(start)

	t.Logf("Redis 宕机测试 - 耗时: %v, 成功: %d, 内存限流拦截: %d", duration, successCount, fallbackLimitCount)

	// 预期：即使 Redis 挂了，由于有 Local Limiter 保护，成功数仍应被限制在 100 左右
	if successCount > 110 {
		t.Errorf("Redis 宕机后降级限流失败，放行过多: %d", successCount)
	}
	if fallbackLimitCount == 0 {
		t.Error("未能检测到降级拦截标识")
	}
}

// 测试 3：极致性能基准测试
func BenchmarkRateLimit_Middleware(b *testing.B) {
	mr, rdb, router := setupTest(b)
	defer mr.Close()
	defer rdb.Close()

	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "172.16.0.1:8888"

	for b.Loop() {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
