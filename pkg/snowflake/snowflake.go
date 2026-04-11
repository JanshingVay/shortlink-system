package snowflake

import (
	"errors"
	"hash/crc32"
	"net"
	"os"
	"sync"
	"time"
)

// | 符号位(1位) | 时间戳(41位) | 节点ID(10位) | 序列号(12位) |
const (
	epoch         int64 = 1_767_225_600_000 //2026-01-01
	nodeBits      uint8 = 10
	stepBits      uint8 = 12
	nodeMax       int64 = -1 ^ (-1 << nodeBits) //1023
	stepMax       int64 = -1 ^ (-1 << stepBits) //4095
	timeShift     uint8 = nodeBits + stepBits   //22
	nodeShift     uint8 = stepBits              //12
	maxBackwardMs int64 = 5                     //5ms
)

type Node struct {
	mu        sync.Mutex
	timestamp int64
	nodeID    int64
	step      int64
}

func NewNode(nodeID int64) (*Node, error) {
	if nodeID < 0 || nodeID > nodeMax {
		return nil, errors.New("node ID out of range")
	}
	return &Node{nodeID: nodeID}, nil
}

// FetchWorkerIDByIP 通过容器/主机的 IP 地址动态计算 Snowflake 算法的 NodeID（WorkerID），适配 K8s 容器化部署场景
// 核心逻辑：
// 1. 优先读取 K8s Downward API 注入的 POD_IP 环境变量（需提前在 Pod YAML 中配置 fieldRef 映射 status.podIP）
// 2. 若 POD_IP 为空（非 K8s 环境/未配置注入），则遍历本地网卡获取第一个有效 IPv4 地址（排除回环地址 127.0.0.1）
// 3. 对有效 IP 执行 CRC32 哈希，将结果映射到 0~1023 区间（匹配 NodeID 10 位的最大取值范围）
// 4. 若所有方式均未获取到有效 IP，返回兜底值 1（避免 NodeID 为空导致算法异常）
//
// 设计考量：
// - 哈希算法选择 CRC32：轻量、计算快，满足分布式场景下 NodeID 唯一性的概率要求
// - 网卡地址过滤：仅取非回环 IPv4，避免 127.0.0.1 导致多节点 NodeID 重复
// - 兜底值设计：1 是合法 NodeID（0~1023），且避开 0 便于区分"未获取IP"和"IP哈希为0"的场景
//
// 返回值：
//
//	int64 - 计算后的 NodeID（范围 0~1023），兜底返回 1
func FetchWorkerIDByIP() int64 {
	// 获取 K8s 注入的 Pod IP，或者读取本地网卡
	ipStr := os.Getenv("POD_IP")
	if ipStr == "" {
		addrs, err := net.InterfaceAddrs()
		if err == nil {
			for _, address := range addrs {
				if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
					ipStr = ipnet.IP.String()
					break
				}
			}
		}
	}

	// 使用 CRC32 哈希将 IP 映射到 0 ~ 1023 的空间内
	if ipStr != "" {
		hash := crc32.ChecksumIEEE([]byte(ipStr))
		return int64(hash) % (nodeMax + 1)
	}
	return 1 // 兜底
}

// Generate 生成符合 Snowflake 算法的分布式唯一 ID
// 核心结构（64位int64）：
// | 时间戳(ms) - 纪元值 | 节点ID(10位) | 序列号(12位) |
//
//	高位剩余位       41位          10位         12位
//
// 注意事项：
// 1. 函数加互斥锁保证单节点内 ID 生成的原子性，避免并发重复
// 2. 严格处理时钟回拨，保障分布式场景下 ID 全局唯一性
// 3. 毫秒内序列号耗尽时会自旋等待下一毫秒，避免 ID 溢出
// 返回值：
//
//	int64: 生成的 Snowflake ID
//	error: 生成失败时返回错误（如严重时钟回拨），成功时为 nil
func (n *Node) Generate() (int64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < n.timestamp {
		backwardDuration := n.timestamp - now
		if backwardDuration <= maxBackwardMs {
			// 短暂时钟回拨，通过自旋等待
			time.Sleep(time.Duration(backwardDuration) * time.Millisecond)
			now = time.Now().UnixMilli()
		} else {
			// 宁可抛错由网关层重试，也不能生成重复 ID
			return 0, errors.New("clock moved backwards severely")
		}
	}

	if now == n.timestamp {
		n.step = (n.step + 1) & stepMax
		if n.step == 0 {
			// 当前毫秒内的序列号耗尽，自旋等待下一毫秒
			for now <= n.timestamp {
				// 避免空转榨干 CPU 资源 (理论上runtime.Gosched() 更好)
				time.Sleep(time.Microsecond)
				now = time.Now().UnixMilli()
			}
		}
	} else {
		n.step = 0
	}

	n.timestamp = now
	id := ((now - epoch) << timeShift) | (n.nodeID << nodeShift) | n.step
	return id, nil
}
