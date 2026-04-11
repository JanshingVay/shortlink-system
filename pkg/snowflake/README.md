# K8s-Aware Snowflake ID Generator

这是一个 Twitter Snowflake (雪花算法) 分布式唯一 ID 生成器。

## ✨ 核心特性

* **动态节点发现 (Dynamic Node ID)**：系统启动时会尝试读取 `POD_IP` 环境变量或遍历宿主机网卡获取 IP，通过 CRC32 哈希映射到 10 bit 的节点空间中。这极大降低了在容器化集群部署时发生 ID 冲突的风险。
* **时钟回拨容忍 (Clock Drift Tolerance)**：在分布式系统中，NTP 时间同步可能会导致极其短暂的时钟回拨。本组件内置了最大 5ms 的容忍窗口，通过自旋等待 (`time.Sleep`) 平滑过渡，确保在极端物理环境下依然能够单调递增，不生成重复 ID。
* **毫秒级并发控制**：在同一毫秒内通过 12 bit 的 Step (序列号) 进行并发控制，单节点理论峰值可达 4096 QPS / ms，支持百万级并发。

## 📊 ID 结构
`1 bit 符号位` | `41 bit 时间戳 (毫秒)` | `10 bit 工作节点 (Hash IP)` | `12 bit 序列号`

## 测试指令（在项目根目录下）：
* 1. 运行所有测试（输出详细日志）
go test -v ./pkg/snowflake/

* 2. 运行性能基准测试（输出 QPS）
go test ./pkg/snowflake/ -bench=. -benchmem