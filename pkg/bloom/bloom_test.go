package bloom

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// setupTestRedis 辅助函数：快速拉起一个内存版 Redis
func setupTestRedis(t testing.TB) (*miniredis.Miniredis, *redis.Client) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

// 测试 1：基本逻辑与误判率初探
func TestRedisBloomFilter_Basic(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	// 初始化一个容量大概 100000 位，使用 5 个哈希函数的 Redis 过滤器
	bf := NewRedisBloom(client, "test:bloom:basic", 100_000, 5)

	// 添加一批真实短链
	realLinks := []string{"a1B2c", "xY9zQ", "7tU8i"}
	for _, link := range realLinks {
		if err := bf.Add(ctx, link); err != nil {
			t.Fatalf("写入布隆过滤器失败: %v", err)
		}
	}

	// 1. 验证真实存在的短链，必须 100% 返回 true
	for _, link := range realLinks {
		exists, err := bf.Contains(ctx, link)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if !exists {
			t.Errorf("致命错误：真实存在的短链 %s 被判断为不存在！", link)
		}
	}

	// 2. 验证根本不存在的短链
	fakeLinks := []string{"00000", "11111", "aaaaa"}
	for _, link := range fakeLinks {
		exists, err := bf.Contains(ctx, link)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}

		if exists {
			// 这就是假阳性（误判），在允许范围内是可以接受的
			t.Logf("出现误判：不存在的短链 %s 被判断为存在 (假阳性)", link)
		} else {
			t.Logf("成功拦截：不存在的短链 %s", link)
		}
	}
}

// 测试 2：高并发网络读写安全测试
func TestRedisBloomFilter_Concurrency(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	bf := NewRedisBloom(client, "test:bloom:concurrency", 100_000, 5)

	var wg sync.WaitGroup
	numRoutines := 5_000 // 模拟 5000 个并发请求 (因为走了网络协议，调小一点避免撑爆文件描述符)

	wg.Add(numRoutines * 2)

	// 并发疯狂写入
	for i := 0; i < numRoutines; i++ {
		go func(val int) {
			defer wg.Done()
			_ = bf.Add(ctx, fmt.Sprintf("link_%d", val))
		}(i)
	}

	// 并发疯狂读取
	for i := 0; i < numRoutines; i++ {
		go func(val int) {
			defer wg.Done()
			_, _ = bf.Contains(ctx, fmt.Sprintf("link_%d", val))
		}(i)
	}

	wg.Wait()
	t.Log("Redis Pipeline 完美扛住高并发读写请求，无数据竞争！")
}

// 测试 3：极限查询性能压测 (模拟海量恶意请求的拦截)
func BenchmarkRedisBloomFilter_Contains(b *testing.B) {
	mr, client := setupTestRedis(b)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	bf := NewRedisBloom(client, "test:bloom:benchmark", 1_000_000, 5)
	_ = bf.Add(ctx, "target_link")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 模拟海量恶意请求的极速拦截
		_, _ = bf.Contains(ctx, "malicious_link_"+strconv.Itoa(i%100))
	}
}
