package model

import "testing"

func TestTableNames(t *testing.T) {
	if (ShortLink{}).TableName() != "short_links" {
		t.Fatal("ShortLink 表名错误")
	}
	if (OutboxMessage{}).TableName() != "outbox_messages" {
		t.Fatal("OutboxMessage 表名错误")
	}
}

func BenchmarkModelConstruction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ShortLink{ID: uint64(i), ShortCode: "abc123", LongURL: "https://example.com"}
	}
}
