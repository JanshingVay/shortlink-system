package base62

import (
	"sync"
	"testing"
)

// 测试 1：基础的编码与解码准确性测试
func TestEncodeDecode(t *testing.T) {
	// 挑选几个边界值和巨大的数字
	testCases := []uint64{0, 1, 61, 62, 123456789, 9876543210, 1729384958271635}

	for _, tc := range testCases {
		encoded := Encode(tc)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("解码失败: %v", err)
		}
		if decoded != tc {
			t.Errorf("转换失败: 期望 %d, 得到 %d (编码为 %s)", tc, decoded, encoded)
		}
		t.Logf("雪花 ID: %-18d -> 生成短链: %-8s -> 逆向还原: %d", tc, encoded, decoded)
	}
}

// 测试 2：高并发验证测试 (十万个并发请求同时转换)
func TestEncode_Concurrency(t *testing.T) {
	var wg sync.WaitGroup
	numRoutines := 100_000
	wg.Add(numRoutines)

	for i := range numRoutines {
		// 启动十万个协程，每个协程转换不同的数字
		go func(val uint64) {
			defer wg.Done()
			encoded := Encode(val)
			decoded, _ := Decode(encoded)
			if decoded != val {
				t.Errorf("并发环境下发生数据错乱: 期望 %d, 得到 %d", val, decoded)
			}
		}(uint64(i))
	}
	wg.Wait()
	t.Logf("成功完成 %d 次并发编解码测试，数据无篡改，纯函数并发安全！", numRoutines)
}

// 测试 3：极限性能压测 (编码)
func BenchmarkEncode(b *testing.B) {
	// 模拟一个典型的 16 位长整数
	num := uint64(1729384958271635)

	for b.Loop() {
		Encode(num)
	}
}

// 测试 4：极限性能压测 (解码)
func BenchmarkDecode(b *testing.B) {
	str := "6qA7Y5"

	for b.Loop() {
		_, _ = Decode(str)
	}
}
