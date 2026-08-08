package cache

import (
	"sync"
	"testing"
	"time"
)

func TestL1CacheLifecycle(t *testing.T) {
	c := NewL1Cache(256, time.Minute)
	c.Set("shortlink:a", "https://example.com/a")

	if got, ok := c.Get("shortlink:a"); !ok || got != "https://example.com/a" {
		t.Fatalf("读取缓存失败: got=%q ok=%v", got, ok)
	}

	c.Set("shortlink:a", "https://example.com/b")
	if got, ok := c.Get("shortlink:a"); !ok || got != "https://example.com/b" {
		t.Fatalf("更新缓存失败: got=%q ok=%v", got, ok)
	}

	c.Invalidate("shortlink:a")
	if _, ok := c.Get("shortlink:a"); ok {
		t.Fatal("Invalidate 后缓存仍然命中")
	}
}

func TestL1CacheExpiryAndNegativeValue(t *testing.T) {
	c := NewL1Cache(256, 10*time.Millisecond)
	c.Set("shortlink:missing", "")
	if got, ok := c.Get("shortlink:missing"); !ok || got != "" {
		t.Fatal("负缓存没有正确保存")
	}

	c.Set("shortlink:expire", "value")
	time.Sleep(25 * time.Millisecond)
	if _, ok := c.Get("shortlink:expire"); ok {
		t.Fatal("过期缓存仍然命中")
	}
}

func TestL1CacheConcurrentAccess(t *testing.T) {
	c := NewL1Cache(4096, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				key := "shortlink:" + string(rune('a'+(worker+j)%26))
				c.Set(key, key)
				_, _ = c.Get(key)
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkL1CacheGetHit(b *testing.B) {
	c := NewL1Cache(100000, time.Minute)
	c.Set("shortlink:hot", "https://example.com")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Get("shortlink:hot")
		}
	})
}

func BenchmarkL1CacheSet(b *testing.B) {
	c := NewL1Cache(100000, time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set("shortlink:hot", "https://example.com")
	}
}
