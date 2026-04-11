# Redis-Backed Distributed Bloom Filter

这是一个基于 Redis Bitmap 实现的分布式布隆过滤器，是短链系统网关层防御**缓存穿透**的终极武器。

## ✨ 核心特性

* **无状态架构 (Stateless)**：将位数组 (Bitset) 状态下沉至 Redis 集群。完美适配 Kubernetes 环境，无论 Pod 如何重启或横向扩缩容 (Scale Out)，拦截状态始终保持一致且不会丢失。
* **网络 I/O 极致优化 (Pipeline)**：布隆过滤器的哈希计算需要对多个位进行读写。本实现全面接入 Redis Pipeline 技术，将 `k` 次独立的网络请求合并为 1 次批量提交，极大地降低了 RTT (往返延迟)。
* **极低内存占用**：通过合理的哈希种子 (k=5) 和容量预估 (m=1,000,000)，在 Redis 中仅消耗几百 KB 的内存，即可实现百万级恶意请求的精确拦截。

## 🛠 原理简述
当请求到达时，系统首先对短码进行 `k` 次独立哈希计算，映射到 Redis Bitmap 的特定偏移量上。若所有位均为 1，则可能存在（放行查缓存）；若任意一位为 0，则绝对不存在（直接拒绝，保护 MySQL）。

# 测试：
* 1.功能测试：
go test -v ./pkg/bloom/
* 2.性能测试：
go test -bench=. -benchmem ./pkg/bloom/