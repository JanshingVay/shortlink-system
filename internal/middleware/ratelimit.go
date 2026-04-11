package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Lua 脚本：实现标准的令牌桶算法
// KEYS[1] = 限流的 Key (如按 IP 或按用户)
// ARGV[1] = 桶的容量 (Capacity)
// ARGV[2] = 令牌生成速率 (Rate/秒)
// ARGV[3] = 当前时间戳 (秒)
// ARGV[4] = 请求消耗的令牌数 (通常是 1)
const tokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- 获取当前桶中的令牌数和上次更新时间
local info = redis.call("HMGET", key, "tokens", "timestamp")
local current_tokens = tonumber(info[1])
local last_timestamp = tonumber(info[2])

-- 如果桶不存在，初始化为满桶
if current_tokens == nil then
    current_tokens = capacity
    last_timestamp = now
end

-- 计算自上次更新以来生成的令牌数
local delta_time = math.max(0, now - last_timestamp)
local generated_tokens = math.floor(delta_time * rate)

-- 更新当前令牌数，但不超过容量
current_tokens = math.min(capacity, current_tokens + generated_tokens)

-- 检查令牌是否足够
if current_tokens >= requested then
    -- 扣减令牌并更新时间戳
    current_tokens = current_tokens - requested
    redis.call("HMSET", key, "tokens", current_tokens, "timestamp", now)
    -- 设置过期时间，防止死 Key 占用内存 (填满桶所需时间 + 10秒余量)
    redis.call("EXPIRE", key, math.ceil(capacity / rate) + 10)
    return 1 -- 允许放行
else
    -- 令牌不足，仅更新时间戳
    redis.call("HMSET", key, "tokens", current_tokens, "timestamp", now)
    redis.call("EXPIRE", key, math.ceil(capacity / rate) + 10)
    return 0 -- 拒绝放行 (限流生效)
end
`

// RateLimitMiddleware 闭包工厂：返回一个 Gin 限流中间件
func RateLimitMiddleware(rdb *redis.Client, capacity, rate int) gin.HandlerFunc {
	// 预加载 Lua 脚本到 Redis，提升执行效率
	script := redis.NewScript(tokenBucketScript)

	return func(c *gin.Context) {
		// 这里以客户端 IP 作为限流维度，实际 AI 网关中通常以 API Key 为维度
		clientIP := c.ClientIP()
		limitKey := fmt.Sprintf("ratelimit:ip:%s", clientIP)

		now := time.Now().Unix()

		// 执行 Lua 脚本
		result, err := script.Run(context.Background(), rdb, []string{limitKey}, capacity, rate, now, 1).Result()
		if err != nil {
			// Redis 故障降级：如果 Redis 崩了，出于高可用原则，我们放行流量而不是瘫痪服务
			fmt.Printf("限流器异常，触发降级放行: %v\n", err)
			c.Next()
			return
		}

		// Lua 脚本返回 0 表示触发限流
		if result.(int64) == 0 {
			// 中断请求，返回 HTTP 429 Too Many Requests
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":  429,
				"error": "请求过于频繁，请稍后再试",
			})
			return
		}

		// 正常放行
		c.Next()
	}
}
