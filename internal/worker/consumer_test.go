package worker

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"shortlink-system/internal/repository"
	"shortlink-system/pkg/bloom"
	"shortlink-system/pkg/cache"
	"shortlink-system/pkg/mq"
)

func TestCacheSyncConsumerHandleMessage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	l1 := cache.NewL1Cache(256, time.Minute)
	urlHashL1 := cache.NewL1Cache(256, time.Minute)
	bf := bloom.NewRedisBloom(client, "test:worker:bloom", 100_000, 5)
	consumer := NewCacheSyncConsumer(&repository.Storage{RedisClient: client}, l1, urlHashL1, bf)
	msg := mq.CacheUpdateMessage{ShortCode: "abc123", LongURL: "https://example.com", URLHash: "hash123"}

	if err := consumer.handleMessage(msg); err != nil {
		t.Fatalf("缓存同步失败: %v", err)
	}
	if got, err := client.Get(context.Background(), "shortlink:abc123").Result(); err != nil || got != msg.LongURL {
		t.Fatalf("正向缓存错误: got=%q err=%v", got, err)
	}
	if got, err := client.Get(context.Background(), "urlhash:hash123").Result(); err != nil || got != msg.ShortCode {
		t.Fatalf("反向缓存错误: got=%q err=%v", got, err)
	}
	if got, ok := l1.Get("shortlink:abc123"); !ok || got != msg.LongURL {
		t.Fatal("L1 正向缓存未同步")
	}
	if got, ok := urlHashL1.Get("urlhash:hash123"); !ok || got != msg.ShortCode {
		t.Fatal("L1 反向缓存未同步")
	}
	if exists, err := bf.Contains(context.Background(), msg.ShortCode); err != nil || !exists {
		t.Fatalf("Bloom 未同步: exists=%v err=%v", exists, err)
	}
}

func TestCacheSyncConsumerRetry(t *testing.T) {
	consumer := &CacheSyncConsumer{}
	attempts := 0
	err := consumer.withRetry(context.Background(), 3, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return context.DeadlineExceeded
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("重试逻辑错误: attempts=%d err=%v", attempts, err)
	}
}

func BenchmarkCacheSyncRetry(b *testing.B) {
	consumer := &CacheSyncConsumer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = consumer.withRetry(context.Background(), 1, func(context.Context) error { return nil })
	}
}
