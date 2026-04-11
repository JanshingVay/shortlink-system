package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HttpRequestsTotal 统计不同接口、方法、状态码的请求总数
	HttpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shortlink_http_requests_total",
			Help: "HTTP 请求总数",
		},
		[]string{"path", "method", "status"},
	)

	// HttpRequestDuration 统计接口延迟分布（P99/P95 核心指标）
	HttpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shortlink_http_request_duration_seconds",
			Help:    "HTTP 请求耗时分布",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5}, // 桶定义，单位秒
		},
		[]string{"path", "method"},
	)

	// RateLimitInterceptionTotal 统计限流拦截次数
	RateLimitInterceptionTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shortlink_ratelimit_interception_total",
			Help: "限流器拦截请求的总数",
		},
		[]string{"path"},
	)

	// BloomFilterInterceptionTotal 统计布隆过滤器拦截次数（防穿透效果指标）
	BloomFilterInterceptionTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shortlink_bloom_filter_interception_total",
			Help: "布隆过滤器成功拦截的非法请求总数",
		},
	)

	// CacheHitTotal 统计 Redis 缓存命中情况
	CacheHitTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shortlink_cache_hit_total",
			Help: "缓存命中次数统计",
		},
		[]string{"hit"}, // hit="true" 或 hit="false"
	)
)
