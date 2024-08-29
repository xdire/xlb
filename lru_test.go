package xlb

import (
	"testing"
	"time"
)

func TestLRUCache(t *testing.T) {

	// Basic operations
	t.Run("Put and Get", func(t *testing.T) {
		cache := NewLRUCache(3)
		cache.Put("127.0.0.1", time.Now().Add(time.Minute), 1)
		cache.Put("192.168.0.1", time.Now().Add(time.Minute), 1)
		cache.Put("10.0.0.1", time.Now().Add(time.Minute), 1)

		entry, ok := cache.Get("192.168.0.1")
		if !ok || entry.IP != "192.168.0.1" {
			t.Errorf("Failed to get the correct entry for key2")
		}
	})

	// Least recently used eviction
	t.Run("Least recently used eviction", func(t *testing.T) {
		cache := NewLRUCache(2)
		cache.Put("127.0.0.1", time.Now().Add(time.Minute), 1)
		cache.Put("192.168.0.1", time.Now().Add(time.Minute), 1)
		cache.Put("10.0.0.1", time.Now().Add(time.Minute), 1)

		_, ok := cache.Get("127.0.0.1")
		if ok {
			t.Errorf("Entry for key1 should have been evicted")
		}
	})

	// Increment count
	t.Run("Increment count", func(t *testing.T) {
		cache := NewLRUCache(2)
		cache.IncrementCount("127.0.0.1", time.Minute)
		cache.IncrementCount("127.0.0.1", time.Minute)

		entry, ok := cache.Get("127.0.0.1")
		if !ok || entry.Count != 2 {
			t.Errorf("Failed to increment the count correctly")
		}
	})

	// Remove expired
	t.Run("Remove expired", func(t *testing.T) {
		cache := NewLRUCache(2)
		cache.Put("127.0.0.1", time.Now().Add(-time.Minute), 1)
		cache.Put("192.168.0.1", time.Now().Add(time.Minute), 1)
		cache.RemoveExpired()

		_, ok := cache.Get("key1")
		if ok {
			t.Errorf("Expired entry for key1 should have been removed")
		}
	})

	// Invalidate
	t.Run("Invalidate", func(t *testing.T) {
		cache := NewLRUCache(2)
		cache.Put("127.0.0.1", time.Now().Add(time.Minute), 1)
		cache.Put("192.168.0.1", time.Now().Add(time.Minute), 1)
		cache.Invalidate("192.168.0.1")

		_, ok := cache.Get("192.168.0.1")
		if ok {
			t.Errorf("Entry for 192.168.0.1 should have been invalidated")
		}
	})
}
