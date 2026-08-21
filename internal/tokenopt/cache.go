package tokenopt

import (
	"container/list"
	"sync"
)

type cacheEntry struct {
	key   string
	value string
}

// lruCache is a bounded, process-local cache. It intentionally never persists
// prompt material to disk.
type lruCache struct {
	capacity int
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

func (c *lruCache) get(key string) (string, bool) {
	if c == nil || c.capacity <= 0 {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(element)
	return element.Value.(cacheEntry).value, true
}

func (c *lruCache) put(key, value string) {
	if c == nil || c.capacity <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		element.Value = cacheEntry{key: key, value: value}
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(cacheEntry{key: key, value: value})
	c.items[key] = element
	if c.order.Len() <= c.capacity {
		return
	}
	oldest := c.order.Back()
	delete(c.items, oldest.Value.(cacheEntry).key)
	c.order.Remove(oldest)
}
