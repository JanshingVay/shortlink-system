package snowflake

import (
	"sync"
	"testing"
	"time"
)

// 测试 1：边界值与非法 NodeID 初始化测试
func TestNewNode_Boundaries(t *testing.T) {
	tests := []struct {
		name    string
		nodeID  int64
		wantErr bool
	}{
		{"正常 NodeID 0", 0, false},
		{"正常 NodeID 1023", 1023, false},
		{"非法 NodeID (负数)", -1, true},
		{"非法 NodeID (超出上限)", 1024, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewNode(tt.nodeID)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNode(%d) 报错情况 = %v, 期望报错 = %v", tt.nodeID, err != nil, tt.wantErr)
			}
		})
	}
}

// 测试 2：轻微时钟回拨测试 (<= maxBackwardMs)
// 期望：触发 time.Sleep，平滑度过回拨期，最终成功生成 ID
func TestNode_ClockBackward_Tolerable(t *testing.T) {
	node, _ := NewNode(1)
	_, _ = node.Generate() // 初始化时间戳

	// 核心技巧：白盒测试，人为篡改内部时间戳，将其拨到未来 3ms
	node.mu.Lock()
	node.timestamp = time.Now().UnixMilli() + 3
	node.mu.Unlock()

	start := time.Now()
	_, err := node.Generate() // 这次生成会发现当前系统时间落后于 node.timestamp 3ms
	duration := time.Since(start)

	if err != nil {
		t.Errorf("期望能容忍 <= 5ms 的回拨，但报错了: %v", err)
	}

	// 验证确实发生了睡眠等待（考虑到调度开销，等待时间应至少接近 3ms）
	if duration < 2*time.Millisecond {
		t.Errorf("期望自旋等待回拨时间，但实际耗时过短: %v", duration)
	}
}

// 测试 3：严重时钟回拨测试 (> maxBackwardMs)
// 期望：直接抛出错误，拒绝生成，防止 ID 重复
func TestNode_ClockBackward_Fatal(t *testing.T) {
	node, _ := NewNode(1)
	_, _ = node.Generate()

	// 人为篡改内部时间戳，将其拨到未来 10ms（超过最大容忍值 5ms）
	node.mu.Lock()
	node.timestamp = time.Now().UnixMilli() + 10
	node.mu.Unlock()

	_, err := node.Generate()
	if err == nil {
		t.Error("期望超过容忍阈值的回拨会拒绝服务并报错，但成功生成了 ID")
	}
}

// 测试 4：同一毫秒内序列号耗尽 (Step Overflow)
// 期望：阻塞等待到下一毫秒，然后继续从 0 开始生成
func TestNode_StepOverflow(t *testing.T) {
	node, _ := NewNode(1)

	// 模拟当前毫秒内的序列号已经被用到了极限 (4095)
	node.mu.Lock()
	node.timestamp = time.Now().UnixMilli()
	node.step = stepMax
	node.mu.Unlock()

	start := time.Now()
	_, err := node.Generate()
	duration := time.Since(start)

	if err != nil {
		t.Errorf("序列号溢出等待时发生错误: %v", err)
	}

	// 验证是否发生了等待（因为要等下一毫秒，至少会有一点延迟）
	if duration.Microseconds() == 0 {
		t.Errorf("期望等待下一毫秒，但耗时为 0")
	}

	// 验证 step 是否重置
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.step != 0 {
		t.Errorf("跨毫秒后 step 应该重置为 0，但实际为 %d", node.step)
	}
}

// 测试 5：高并发唯一性测试 (十万个并发请求)
func TestNode_Generate_Concurrency(t *testing.T) {
	node, err := NewNode(1)
	if err != nil {
		t.Fatalf("初始化节点失败: %v", err)
	}

	var wg sync.WaitGroup
	var idMap sync.Map // 并发安全的 Map 记录生成的 ID

	numRoutines := 100_000
	wg.Add(numRoutines)

	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()
			id, err := node.Generate()
			if err != nil {
				t.Errorf("生成 ID 报错: %v", err)
				return
			}

			if _, exists := idMap.LoadOrStore(id, true); exists {
				t.Errorf("发生灾难级错误！生成了重复的 ID: %d", id)
			}
		}()
	}

	wg.Wait()
	t.Logf("成功完成 %d 次并发测试，没有产生重复 ID！", numRoutines)
}

// 测试 6：极限性能基准测试
func BenchmarkNode_Generate(b *testing.B) {
	node, _ := NewNode(1)
	b.ResetTimer() // 重置计时器，排除初始化时间

	for i := 0; i < b.N; i++ {
		_, _ = node.Generate()
	}
}
