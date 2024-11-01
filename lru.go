package xlb

import (
	"container/list"
	"sync"
	"time"
)

type CacheEntry struct {
	IP        string
	ExpiresAt time.Time
	Count     int
}

// LRUCache simple LRU cache to store, search and easily evict data
type LRUCache struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
	mutex    sync.RWMutex
}

// NewLRUCache Constructor for LRU cache
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get looks up value in the cache and returns it
func (c *LRUCache) Get(key string) (*CacheEntry, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if entry, ok := c.cache[key]; ok {
		return entry.Value.(*CacheEntry), true
	}
	return nil, false
}

// Put adds value to the cache
func (c *LRUCache) Put(key string, t time.Time, count int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.put(key, t, count)
}

// method to provide outside locking for more than one transaction in cache
func (c *LRUCache) put(key string, t time.Time, count int) {
	if entry, ok := c.cache[key]; ok {
		c.list.MoveToFront(entry)
		entry.Value = &CacheEntry{key, t, count}
		return
	}

	value := &CacheEntry{key, t, count}
	newEntry := c.list.PushFront(value)
	c.cache[key] = newEntry

	if c.list.Len() > c.capacity {
		lastEntry := c.list.Back()
		c.list.Remove(lastEntry)
		delete(c.cache, lastEntry.Value.(*CacheEntry).IP)
	}
}

// IncrementCount increments counts for some record in the cache
func (c *LRUCache) IncrementCount(ip string, blockDuration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if entry, ok := c.cache[ip]; ok {
		entry.Value.(*CacheEntry).Count++
		entry.Value.(*CacheEntry).ExpiresAt = time.Now().Add(blockDuration * time.Duration(entry.Value.(*CacheEntry).Count))
		c.list.MoveToFront(entry)
	} else {
		c.put(ip, time.Now().Add(blockDuration), 1)
	}
}

// Invalidate particular element in the cache
func (c *LRUCache) Invalidate(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if entry, ok := c.cache[key]; ok {
		c.list.Remove(entry)
		delete(c.cache, key)
	}
}

// RemoveExpired bulk remove of expired records
func (c *LRUCache) RemoveExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for c.list.Len() > 0 {
		entry := c.list.Back().Value.(*CacheEntry)
		if entry.ExpiresAt.Before(time.Now()) {
			c.list.Remove(c.list.Back())
			delete(c.cache, entry.IP)
		} else {
			break
		}
	}
}
