package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestExtractHashTagKey(t *testing.T) {
	tests := map[string]string{
		"{user}:profile":   "user",
		"{shortlink}:code": "shortlink",
		"plain-key":        "plain-key",
		"{}:empty":         "",
		"{broken":          "{broken",
	}
	for input, want := range tests {
		if got := ExtractHashTagKey(input); got != want {
			t.Errorf("ExtractHashTagKey(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestBatchRedisSetAndGet(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()

	storage := &Storage{RedisClient: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	defer storage.RedisClient.(*redis.Client).Close()
	pairs := map[string]string{"k:a": "a", "k:b": "b", "k:c": "c"}
	if err := storage.BatchRedisSet(context.Background(), pairs, time.Minute); err != nil {
		t.Fatalf("批量写 Redis 失败: %v", err)
	}
	got, err := storage.BatchRedisGet(context.Background(), []string{"k:a", "k:b", "k:c", "k:missing"})
	if err != nil {
		t.Fatalf("批量读 Redis 失败: %v", err)
	}
	if len(got) != len(pairs) || got["k:b"] != "b" {
		t.Fatalf("批量读结果错误: %#v", got)
	}
}

func BenchmarkBatchRedisGet(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	storage := &Storage{RedisClient: client}
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = "bench:" + string(rune('a'+i))
		_ = client.Set(context.Background(), keys[i], "value", time.Minute).Err()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = storage.BatchRedisGet(context.Background(), keys)
	}
}
