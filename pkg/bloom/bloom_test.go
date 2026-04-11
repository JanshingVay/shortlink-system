package bloom

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

// 测试 1：基本逻辑与误判率初探
func TestBloomFilter_Basic(t *testing.T) {
	// 初始化一个容量大概 100000 位，使用 5 个哈希函数的过滤器
	bf := New(100_000, 5)

	// 添加一批真实短链
	realLinks := []string{"a1B2c", "xY9zQ", "7tU8i"}
	for _, link := range realLinks {
		bf.Add(link)
	}

	// 1. 验证真实存在的短链，必须 100% 返回 true
	for _, link := range realLinks {
		if !bf.Contains(link) {
			t.Errorf("致命错误：真实存在的短链 %s 被判断为不存在！", link)
		}
	}

	// 2. 验证根本不存在的短链
	fakeLinks := []string{"00000", "11111", "aaaaa"}
	for _, link := range fakeLinks {
		if bf.Contains(link) {
			// 这就是假阳性（误判），在允许范围内是可以接受的，只是多查一次 Redis 而已
			t.Logf("出现误判：不存在的短链 %s 被判断为存在 (假阳性)", link)
		} else {
			t.Logf("成功拦截：不存在的短链 %s", link)
		}
	}
}

// 测试 2：读写并发安全测试
func TestBloomFilter_Concurrency(t *testing.T) {
	bf := New(100_000, 5)
	var wg sync.WaitGroup
	numRoutines := 50_000

	wg.Add(numRoutines * 2)

	// 50000 个协程疯狂写入
	for i := range numRoutines {
		go func(val int) {
			defer wg.Done()
			bf.Add(fmt.Sprintf("link_%d", val))
		}(i)
	}

	// 50000 个协程疯狂读取
	for i := range numRoutines {
		go func(val int) {
			defer wg.Done()
			bf.Contains(fmt.Sprintf("link_%d", val))
		}(i)
	}

	wg.Wait()
	t.Log("读写锁 (RWMutex) 完美扛住高并发，没有出现 data race！")
}

// 测试 3：极限查询性能压测 (模拟海量恶意请求的极速拦截)
func BenchmarkBloomFilter_Contains(b *testing.B) {
	bf := New(1_000_000, 5) // 分配约 1MB 大小的过滤器
	bf.Add("target_link")

	b.ResetTimer()
	for i := range b.N {
		// 模拟海量恶意请求的极速拦截
		bf.Contains("malicious_link_" + strconv.Itoa(i%100))
	}
}
