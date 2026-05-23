package cache

import (
	"container/list"
	"sync"
	"time"
)

type l1Entry struct {
	key     string
	value   string
	expireAt time.Time
}

type L1Cache struct {
	mu       sync.RWMutex
	capacity int
	ttl      time.Duration
	items    map[string]*list.Element
	evictList *list.List
}

func NewL1Cache(capacity int, ttl time.Duration) *L1Cache {
	return &L1Cache{
		capacity:  capacity,
		ttl:       ttl,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

func (c *L1Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	elem, ok := c.items[key]
	if !ok {
		c.mu.RUnlock()
		return "", false
	}

	entry := elem.Value.(*l1Entry)
	if time.Now().After(entry.expireAt) {
		c.mu.RUnlock()
		c.mu.Lock()
		if elem2, ok2 := c.items[key]; ok2 {
			if time.Now().After(elem2.Value.(*l1Entry).expireAt) {
				c.evictList.Remove(elem2)
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
		return "", false
	}
	c.mu.RUnlock()

	c.mu.Lock()
	c.evictList.MoveToFront(elem)
	c.mu.Unlock()

	return entry.value, true
}

func (c *L1Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		entry := elem.Value.(*l1Entry)
		entry.value = value
		entry.expireAt = time.Now().Add(c.ttl)
		return
	}

	entry := &l1Entry{
		key:     key,
		value:   value,
		expireAt: time.Now().Add(c.ttl),
	}
	elem := c.evictList.PushFront(entry)
	c.items[key] = elem

	for c.evictList.Len() > c.capacity {
		c.evict()
	}
}

func (c *L1Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.Remove(elem)
		delete(c.items, key)
	}
}

func (c *L1Cache) evict() {
	elem := c.evictList.Back()
	if elem != nil {
		entry := elem.Value.(*l1Entry)
		c.evictList.Remove(elem)
		delete(c.items, entry.key)
	}
}
