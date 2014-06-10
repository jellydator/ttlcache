package ttlcache

import (
	"sync"
	"time"
)

type Cache struct {
	sync.RWMutex
	TTL   time.Duration
	items map[string]*Item
}

func (cache *Cache) Set(key string, data string) {
	cache.Lock()
	item := &Item{data: data}
	item.Touch(cache.TTL)
	cache.items[key] = item
	cache.Unlock()
}

func (cache *Cache) Get(key string) (data string, found bool) {
	cache.Lock()
	item, exists := cache.items[key]
	if !exists || item.Expired() {
		data = ""
		found = false
	} else {
		item.Touch(cache.TTL)
		data = item.data
		found = true
	}
	cache.Unlock()
	return
}

func (cache *Cache) Count() int {
	cache.RLock()
	count := len(cache.items)
	cache.RUnlock()
	return count
}

func (cache *Cache) Cleanup() {
	cache.Lock()
	for key, item := range cache.items {
		if item.Expired() {
			delete(cache.items, key)
		}
	}
	cache.Unlock()
}

func (cache *Cache) startCleanupTimer() {
	duration := cache.TTL * 2
	if duration < time.Second {
		duration = time.Second
	}
	ticker := time.Tick(duration)
	go (func() {
		for {
			select {
			case <-ticker:
				cache.Cleanup()
			}
		}
	})()
}

func NewCache(duration time.Duration) *Cache {
	cache := &Cache{
		TTL:   duration,
		items: map[string]*Item{},
	}
	cache.startCleanupTimer()
	return cache
}
