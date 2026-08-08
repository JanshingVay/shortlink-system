package cache

import (
	"container/list"
	"hash/fnv"
	"sync"
	"time"
)

type l1Entry struct {
	key      string
	value    string
	expireAt time.Time
}

// defaultShardCount 分片数，必须是 2 的幂，以便用位与取模。
// 将全局锁拆分为 N 个独立分片锁后，热点 key 的锁争用约降低 N 倍，
// 同时把"读 + LRU 前移"放到同一把锁内，消除原实现释放锁后再 MoveToFront 的竞态。
const defaultShardCount = 256

// cacheShard 单个分片：自持有 map + LRU 链表 + 互斥锁。
type cacheShard struct {
	mu        sync.Mutex
	capacity  int
	ttl       time.Duration
	items     map[string]*list.Element
	evictList *list.List
}

// L1Cache 进程内本地缓存，采用分片锁降低高并发下的锁争用。
type L1Cache struct {
	shards    []*cacheShard
	shardMask uint64
}

func NewL1Cache(capacity int, ttl time.Duration) *L1Cache {
	shardCount := defaultShardCount
	perShard := capacity / shardCount
	if perShard < 1 {
		perShard = 1
	}

	shards := make([]*cacheShard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i] = &cacheShard{
			capacity:  perShard,
			ttl:       ttl,
			items:     make(map[string]*list.Element, perShard),
			evictList: list.New(),
		}
	}

	return &L1Cache{
		shards:    shards,
		shardMask: uint64(shardCount - 1),
	}
}

// getShard 通过 FNV-1a 哈希将 key 稳定映射到某个分片。
func (c *L1Cache) getShard(key string) *cacheShard {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return c.shards[h.Sum64()&c.shardMask]
}

// Get 读取缓存。命中后顺带把节点提到链表头部（LRU），
// 整个读 + 前移在分片锁内完成，避免并发下移动已被驱逐的陈旧元素。
func (s *cacheShard) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.items[key]
	if !ok {
		return "", false
	}

	entry := elem.Value.(*l1Entry)
	if time.Now().After(entry.expireAt) {
		// 惰性过期：命中时发现已失效，直接淘汰
		s.evictList.Remove(elem)
		delete(s.items, key)
		return "", false
	}

	s.evictList.MoveToFront(elem)
	return entry.value, true
}

// Set 写入或更新缓存，并在超过分片容量时从链表尾部淘汰。
func (s *cacheShard) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.items[key]; ok {
		s.evictList.MoveToFront(elem)
		entry := elem.Value.(*l1Entry)
		entry.value = value
		entry.expireAt = time.Now().Add(s.ttl)
		return
	}

	entry := &l1Entry{
		key:      key,
		value:    value,
		expireAt: time.Now().Add(s.ttl),
	}
	elem := s.evictList.PushFront(entry)
	s.items[key] = elem

	for s.evictList.Len() > s.capacity {
		s.evict()
	}
}

// Invalidate 精确删除某个 key。
func (s *cacheShard) Invalidate(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.items[key]; ok {
		s.evictList.Remove(elem)
		delete(s.items, key)
	}
}

// evict 淘汰链表尾部的节点（最久未使用）。调用方需持有分片锁。
func (s *cacheShard) evict() {
	elem := s.evictList.Back()
	if elem == nil {
		return
	}
	entry := elem.Value.(*l1Entry)
	s.evictList.Remove(elem)
	delete(s.items, entry.key)
}

// Get 对外只读接口：按分片路由。
func (c *L1Cache) Get(key string) (string, bool) {
	return c.getShard(key).Get(key)
}

// Set 对外写接口：按分片路由。
func (c *L1Cache) Set(key, value string) {
	c.getShard(key).Set(key, value)
}

// Invalidate 对外删除接口：按分片路由。
func (c *L1Cache) Invalidate(key string) {
	c.getShard(key).Invalidate(key)
}
