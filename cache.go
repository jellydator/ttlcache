package ttlcache

import (
	"errors"
	"sync"
	"time"
)

// ExpireCallback is used as a callback on item expiration
type ExpireCallback func(key string, value interface{})

// Cache is a synchronized map of items that auto-expire once stale
type Cache struct {
	mutex          sync.RWMutex
	ttl            time.Duration
	items          map[string]*Item
	expireCallback ExpireCallback
	expireTick     <-chan time.Time // FIX: precisa estar aqui!?
}

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {
	return &Cache{items: map[string]*Item{}}
}

// Set is a thread-safe way to add new items to the map
func (cache *Cache) Set(key string, data interface{}) {
	cache.SetWithTTL(key, data, cache.ttl)
}

// SetWithTTL is a thread-safe way to add new items to the map with individual ttl
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item := &Item{data: data, ttl: &ttl}
	item.touch()
	cache.items[key] = item
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (data interface{}, found bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item, exists := cache.items[key]
	if !exists || item.expired() {
		return nil, false
	}
	item.touch()
	return item.data, exists
}

// Count returns the number of items in the cache
func (cache *Cache) Count() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return len(cache.items)
}

func (cache *Cache) cleanup() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	for key, item := range cache.items {
		if item.expired() {
			if cache.expireCallback != nil {
				cache.expireCallback(key, item)
			}
			delete(cache.items, key)
		}
	}
}

func (cache *Cache) startCleanupTimer() {
	go func() {
		for {
			select {
			case <-cache.expireTick:
				cache.cleanup()
			}
		}
	}()
}

// SetTimeout set the default timeout and initialize the previous items with empty timeout with the new
func (cache *Cache) SetTimeout(ttl, cleanupPeriod time.Duration) error {
	if cache.expireTick != nil {
		return errors.New("Timeout already initialized")
	}
	if cleanupPeriod == 0 {
		cleanupPeriod = time.Duration(1 * time.Second)
	}
	cache.ttl = ttl
	cache.expireTick = time.Tick(cleanupPeriod)
	cache.startCleanupTimer()
	return nil
}

// SetExpireCallback is used to save the callback that is called on key expire
func (cache *Cache) SetExpireCallback(callback ExpireCallback) {
	cache.expireCallback = callback
}
