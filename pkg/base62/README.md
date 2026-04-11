# Base62 Encoder & Decoder

本项目提供了一个专为超高并发场景优化的 Base62 短码转换工具包。

## ✨ 核心特性

* **零内存分配 (Zero-Allocation)**：在 `Encode` 过程中，摒弃了传统的 `slice` 动态扩容和 `reverse` 翻转操作。通过精确计算 uint64 在 62 进制下的最大长度（11 位），采用固定长度的局部数组 `[11]byte` 进行逆向填充。成功避免了变量逃逸到堆区 (Heap Escape)，将 GC 压力降至最低。
* **防预测安全机制**：废弃了 `0-9a-zA-Z` 的标准字典顺序，内置了自定义的乱序字典 (Shuffle Alphabet)。这有效防止了黑客通过递增雪花 ID 规律来爬取或遍历系统的短链数据（Security through obscurity）。
* **纯函数并发安全**：无任何包级全局可变状态，原生支持海量 Goroutine 并发调用，无需加锁。

## 🚀 性能指标
在 Benchmark 测试中，单次 Encode/Decode 耗时极低，完全满足系统达到 SSP 级别的吞吐量要求。

## 测试：
* 1.功能测试：
go test -v ./pkg/base62/
* 2.性能测试:
go test -v -bench=. -benchmem ./pkg/base62