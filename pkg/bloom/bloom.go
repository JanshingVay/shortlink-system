package bloom

import (
	"sync"
)

// 布隆过滤器核心结构
type BloomFilter struct {
	mu    sync.RWMutex // 读写锁：高并发下读多写少，RWMutex 性能更好
	bits  []uint64     // 位数组：使用 uint64 切片来模拟，比直接用 bool 数组节省 64 倍内存
	size  uint64       // 位数组的总比特数 (必须是 64 的整数倍)
	seeds []uint32     // 不同的哈希种子，代表 k 个不同的哈希函数
}

// New 创建一个新的布隆过滤器
// size: 预计的数据量大小； hashCount: 哈希函数的数量
func New(size uint64, hashCount int) *BloomFilter {
	// 确保 size 是 64 的倍数，方便映射到 uint64 上
	bitSize := (size/64 + 1) * 64

	// 生成 k 个不同的质数作为哈希种子
	seeds := make([]uint32, hashCount)
	basePrimes := []uint32{7, 11, 13, 31, 37, 61, 89, 97, 101, 107} // 常用质数
	for i := range hashCount {
		seeds[i] = basePrimes[i%len(basePrimes)] + uint32(i)
	}

	return &BloomFilter{
		bits:  make([]uint64, bitSize/64),
		size:  bitSize,
		seeds: seeds,
	}
}

// Add 将一个字符串（短链）加入布隆过滤器
func (bf *BloomFilter) Add(str string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for _, seed := range bf.seeds {
		hashVal := bf.hash(str, seed)
		position := hashVal % bf.size

		// 计算在哪个 uint64 上
		index := position / 64
		// 计算在这个 uint64 的第几位 (0-63)
		offset := position % 64

		// 将对应位置 1 (位运算：按位或)
		bf.bits[index] |= (1 << offset)
	}
}

// Contains 判断一个字符串（短链）是否“可能存在”
// 返回 false 代表绝对不存在；返回 true 代表可能存在（有极小概率的误判）
func (bf *BloomFilter) Contains(str string) bool {
	bf.mu.RLock() // 读锁，允许多个协程同时查询
	defer bf.mu.RUnlock()

	for _, seed := range bf.seeds {
		hashVal := bf.hash(str, seed)
		position := hashVal % bf.size

		index := position / 64
		offset := position % 64

		// 检查对应位是否为 1 (位运算：按位与)
		// 如果有一个位置为 0，说明绝对不存在
		if (bf.bits[index] & (1 << offset)) == 0 {
			return false
		}
	}
	return true // 所有位都是 1，可能存在
}

// hash 一个非常简单且高效的字符串哈希算法 (BKDR Hash 的变种)
func (bf *BloomFilter) hash(str string, seed uint32) uint64 {
	var hash uint64 = 0
	for i := 0; i < len(str); i++ {
		hash = hash*uint64(seed) + uint64(str[i])
	}
	return hash
}
