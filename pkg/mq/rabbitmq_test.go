package mq

import (
	"encoding/json"
	"testing"
)

func TestCacheUpdateMessageJSON(t *testing.T) {
	want := CacheUpdateMessage{ShortCode: "abc123", LongURL: "https://example.com", URLHash: "hash"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("消息序列化失败: %v", err)
	}
	var got CacheUpdateMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("消息反序列化失败: %v", err)
	}
	if got != want {
		t.Fatalf("消息内容不一致: got=%+v want=%+v", got, want)
	}
	if ExchangeName == "" || QueueName == "" || RoutingKey == "" {
		t.Fatal("RabbitMQ 拓扑常量不能为空")
	}
}

func BenchmarkCacheUpdateMessageJSON(b *testing.B) {
	msg := CacheUpdateMessage{ShortCode: "abc123", LongURL: "https://example.com", URLHash: "hash"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(msg)
	}
}
