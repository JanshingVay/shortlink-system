# 最新测试报告

测试时间：2026-08-08 20:55:40 CST

测试环境：macOS，Apple M2，darwin/arm64，Go 1.26

## 测试范围

本次根据最新代码补充或更新了以下模块的功能测试与性能基准：

- `internal/api`：错误响应格式、请求结构、响应基准
- `internal/middleware`：Redis 正常限流、Redis 故障降级、并发限流、性能基准
- `internal/model`：表名映射、模型构造基准
- `internal/repository`：Redis 批量读写、Hash Tag 解析、批量 Redis 读取基准
- `internal/service`：MD5、L1 命中重定向、MD5 和 L1 重定向基准
- `internal/worker`：缓存同步 Pipeline、反向缓存、Bloom 同步、重试逻辑和基准
- `pkg/base62`：编码解码功能、并发安全、编码解码基准
- `pkg/bloom`：Bloom 增删查、并发访问、Lua 查询基准
- `pkg/cache`：L1 缓存生命周期、过期、负缓存、并发访问、读写基准
- `pkg/config`：环境变量覆盖、配置加载基准
- `pkg/mq`：消息 JSON 序列化、拓扑常量、序列化基准
- `pkg/snowflake`：边界、时钟回拨、序列溢出、并发唯一性、生成基准

Redis 相关测试使用 `miniredis`，不依赖外部 Redis 服务；RabbitMQ 和 MySQL 的真实连接集成测试未在本次本地测试中启动。

## 功能测试

执行命令：

```bash
go test ./...
```

结果：通过。

主要结果：

```text
shortlink-system/internal/api         ok
shortlink-system/internal/middleware  ok
shortlink-system/internal/model       ok
shortlink-system/internal/repository  ok
shortlink-system/internal/service     ok
shortlink-system/internal/worker      ok
shortlink-system/pkg/base62            ok
shortlink-system/pkg/bloom             ok
shortlink-system/pkg/cache             ok
shortlink-system/pkg/config            ok
shortlink-system/pkg/mq                ok
shortlink-system/pkg/snowflake         ok
```

## 竞态测试

执行命令：

```bash
go test -race ./...
```

结果：通过，未发现数据竞争。

## 性能测试

执行命令：

```bash
go test -run '^$' -bench=. -benchmem -benchtime=500ms -count=1 ./...
```

基准结果：

| 模块 | Benchmark | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| API | `BenchmarkRespondError` | 1,010 | 2,625 | 26 |
| Middleware | `BenchmarkRateLimit_Middleware` | 92,668 | 201,739 | 849 |
| Model | `BenchmarkModelConstruction` | 0.2939 | 0 | 0 |
| Repository | `BenchmarkBatchRedisGet` | 129,037 | 14,119 | 532 |
| Service | `BenchmarkMD5Hex` | 142.0 | 80 | 2 |
| Service | `BenchmarkRedirectL1Hit` | 73.03 | 16 | 1 |
| Worker | `BenchmarkCacheSyncRetry` | 2.072 | 0 | 0 |
| Base62 | `BenchmarkEncode` | 10.48 | 0 | 0 |
| Base62 | `BenchmarkDecode` | 15.48 | 0 | 0 |
| Bloom | `BenchmarkRedisBloomFilter_Contains` | 71,171 | 187,399 | 771 |
| Cache | `BenchmarkL1CacheGetHit` | 148.2 | 0 | 0 |
| Cache | `BenchmarkL1CacheSet` | 56.90 | 0 | 0 |
| Config | `BenchmarkLoadConfig` | 57,225 | 39,370 | 661 |
| MQ | `BenchmarkCacheUpdateMessageJSON` | 119.4 | 80 | 1 |
| Snowflake | `BenchmarkNode_Generate` | 243.9 | 0 | 0 |

Redis、miniredis 和 HTTP 基准包含本地测试服务通信成本，不能直接等同于生产网络环境下的 QPS；生产性能仍需使用真实 Redis Cluster、MySQL 和 RabbitMQ 进行压测。

## 静态检查

```bash
go vet ./...
git diff --check
```

结果：均通过。

## 备注

本次还修正了限流并发测试中固定假设“成功数必须接近初始容量”的不稳定断言。令牌桶会随真实时间补充令牌，在 `-race` 或低速 CI 环境下成功数可能合法增加，因此测试现在验证限流确实生效，而不依赖具体调度时序。
