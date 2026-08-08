package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"shortlink-system/internal/repository"
)

func TestMD5Hex(t *testing.T) {
	if got := md5Hex("hello"); got != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("MD5 结果错误: %s", got)
	}
}

func TestRedirectL1Hit(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	svc, err := NewShortLinkService("http://localhost/", &repository.Storage{RedisClient: client})
	if err != nil {
		t.Fatalf("创建 service 失败: %v", err)
	}
	svc.L1Cache().Set("shortlink:abc123", "https://example.com")

	got, err := svc.Redirect(context.Background(), "abc123")
	if err != nil || got != "https://example.com" {
		t.Fatalf("L1 重定向失败: got=%q err=%v", got, err)
	}
}

func BenchmarkMD5Hex(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = md5Hex("https://example.com/path/to/resource?id=123456")
	}
}

func BenchmarkRedirectL1Hit(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	svc, err := NewShortLinkService("http://localhost/", &repository.Storage{RedisClient: client})
	if err != nil {
		b.Fatalf("创建 service 失败: %v", err)
	}
	svc.L1Cache().Set("shortlink:bench", "https://example.com")
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Redirect(ctx, "bench")
	}
}
