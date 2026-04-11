package snowflake

import (
	"sync"
	"testing"
)

// 测试 1：高并发唯一性测试 (模拟一万个并发请求)
func TestNode_Generate_Concurrency(t *testing.T) {
	node, err := NewNode(1) // 假设这是 1 号机器
	if err != nil {
		t.Fatalf("初始化节点失败: %v", err)
	}

	var wg sync.WaitGroup
	var idMap sync.Map // 使用并发安全的 sync.Map 来记录生成的 ID

	// 模拟 100,000 个协程同时请求 ID
	numRoutines := 100_000
	wg.Add(numRoutines)

	for range numRoutines {
		go func() {
			defer wg.Done()
			id, err := node.Generate()
			if err != nil {
				t.Errorf("生成 ID 报错: %v", err)
				return
			}

			// 尝试把生成的 ID 存入 Map
			// LoadOrStore 如果发现 ID 已经存在，exists 会返回 true
			if _, exists := idMap.LoadOrStore(id, true); exists {
				t.Errorf("发生灾难级错误！生成了重复的 ID: %d", id)
			}
		}()
	}

	wg.Wait() // 阻塞等待所有协程执行完毕
	t.Logf("成功完成 %d 次并发测试，没有产生重复 ID！", numRoutines)
}

// 测试 2：极限性能基准测试
func BenchmarkNode_Generate(b *testing.B) {
	node, _ := NewNode(1)
	b.ResetTimer() // 重置计时器，排除初始化时间

	for range b.N {
		_, _ = node.Generate()
	}
}
