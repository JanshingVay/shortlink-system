package bloom

import (
	"context"

	"github.com/redis/go-redis/v9"
)

var basePrimes = []uint32{7, 11, 13, 31, 37, 61, 89, 97, 101, 107}

// RedisBloomFilter 分布式布隆过滤器
type RedisBloomFilter struct {
	client *redis.Client
	key    string
	size   uint64
	seeds  []uint32
}

// NewRedisBloom 创建基于 Redis 的分布式布隆过滤器实例
// 核心设计：
// 1. 基于素数种子生成多组独立哈希函数，保证哈希分布均匀性，降低误判率
// 2. 预生成哈希种子列表，避免运行时重复计算，提升性能
// 3. 适配 Redis Pipeline 批量操作，最大化网络传输效率
//
// 参数说明：
//
//	client: Redis 客户端实例（建议复用全局客户端，减少连接开销）
//	key: Redis 中布隆过滤器对应的 Key 名（需保证唯一性，避免不同过滤器冲突）
//	size: 布隆过滤器的 BitMap 总长度（单位：位），决定过滤器容量，建议根据预期存储元素数和误判率估算
//	hashCount: 哈希函数数量，需平衡误判率和空间占用（推荐值：5~8）
//
// 返回值：
//
//	*RedisBloomFilter: 初始化完成的分布式布隆过滤器实例
//
// 性能注意事项：
//   - hashCount 不宜过大（>10），否则会增加 Redis Pipeline 指令数，提升 RTT 延迟
//   - size 建议按 2^n 对齐（如 1<<20），减少取模运算开销
//   - 单实例可复用，无需频繁创建（seeds 预生成，线程安全）
func NewRedisBloom(client *redis.Client, key string, size uint64, hashCount int) *RedisBloomFilter {
	seeds := make([]uint32, hashCount)
	for i := 0; i < hashCount; i++ {
		seeds[i] = basePrimes[i%len(basePrimes)] + uint32(i)
	}

	return &RedisBloomFilter{
		client: client,
		key:    key,
		size:   size,
		seeds:  seeds,
	}
}

// Add 向分布式布隆过滤器中添加指定字符串
// 核心逻辑：
// 1. 遍历预生成的哈希种子，为每个种子计算字符串对应的哈希值
// 2. 将哈希值取模映射到 BitMap 有效偏移位范围内
// 3. 通过 Redis Pipeline 批量执行 SetBit 命令，将所有目标位设为 1
//
// 性能优化：
// - 复用 Pipeline 批量操作，将 N 次网络请求（N=哈希函数数量）压缩为 1 次，极致降低网络 RTT 开销
// - 哈希计算在本地完成，仅将最终置位指令发送到 Redis，减少网络传输量
//
// 参数：
//
//	ctx: 上下文，用于控制 Redis 操作的超时、取消（适配分布式系统的链路管控）
//	str: 要添加到布隆过滤器的字符串（如短链标识、业务唯一键等）
//
// 返回值：
//
//	error: 执行过程中的错误（如 Redis 网络错误、超时、连接异常等），nil 表示添加成功
//
// 注意事项：
// - 该方法是幂等的：重复添加同一字符串不会产生副作用（Bit 位已为 1 时再次置 1 无意义）
// - 建议通过 ctx.WithTimeout 控制超时，避免阻塞业务流程
func (bf *RedisBloomFilter) Add(ctx context.Context, str string) error {
	pipe := bf.client.Pipeline()
	for _, seed := range bf.seeds {
		hashVal := bf.hash(str, seed)
		offset := int64(hashVal % bf.size)
		pipe.SetBit(ctx, bf.key, offset, 1)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// Contains 检查指定字符串是否存在于分布式布隆过滤器中
// 核心原理：
// 1. 布隆过滤器特性：若某字符串对应的任意一个 Bit 位为 0，则该字符串一定不存在；若所有位都为 1，仅表示“可能存在”（存在误判率）
// 2. 性能优化：通过 Redis Pipeline 批量执行 GetBit 命令，将 N 次网络请求（N=哈希函数数量）压缩为 1 次，大幅降低网络 RTT 开销
//
// 参数：
//
//	ctx: 上下文，用于控制 Redis 操作的超时、取消（适配分布式系统链路管控）
//	str: 待检查是否存在的字符串（如短链标识、业务唯一键等）
//
// 返回值：
//
//	bool: 检查结果，false 表示“绝对不存在”，true 表示“可能存在”（受布隆过滤器误判率影响）
//	error: 执行过程中的错误（如 Redis 网络错误、超时、连接异常等），nil 表示检查流程正常完成
//
// 注意事项：
// - 结果为 true 时不代表字符串“一定存在”，仅为概率性判断（误判率由初始化时的 size 和 hashCount 决定）
// - 结果为 false 时是 100% 准确的，可放心用于“不存在”的判断（如缓存穿透拦截）
func (bf *RedisBloomFilter) Contains(ctx context.Context, str string) (bool, error) {
	pipe := bf.client.Pipeline()
	cmds := make([]*redis.IntCmd, len(bf.seeds))

	for i, seed := range bf.seeds {
		hashVal := bf.hash(str, seed)
		offset := int64(hashVal % bf.size)
		cmds[i] = pipe.GetBit(ctx, bf.key, offset)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	// 只要有一个位是 0，就绝对不存在
	for _, cmd := range cmds {
		if cmd.Val() == 0 {
			return false, nil
		}
	}
	return true, nil
}

// hash 基于指定种子对字符串进行自定义哈希计算
// 核心算法：线性累加哈希（乘法+加法），遍历字符串每个字符的ASCII值，结合种子生成唯一哈希值
// 设计目的：不同种子生成不同哈希结果，保证布隆过滤器多哈希函数的独立性，降低误判率
//
// 参数：
//
//	str: 待计算哈希的目标字符串
//	seed: 哈希种子（不同种子对应布隆过滤器的不同哈希函数）
//
// 返回值：
//
//	uint64: 计算得到的哈希值，用于后续映射到BitMap偏移位
//
// 算法特点：
//  1. 计算轻量：仅通过基础算术运算完成，无复杂逻辑，性能开销低
//  2. 种子敏感：相同字符串+不同种子会生成差异显著的哈希值，保证分散性
//  3. 无哈希冲突优化：布隆过滤器本身允许一定误判率，无需额外处理哈希冲突
func (bf *RedisBloomFilter) hash(str string, seed uint32) uint64 {
	var hash uint64 = 0
	for i := 0; i < len(str); i++ {
		hash = hash*uint64(seed) + uint64(str[i])
	}
	return hash
}
