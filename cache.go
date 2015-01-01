package ttlcache

import (
	"sync"
	"time"
)

// ExpireCallback is used as a callback on item expiration
type ExpireCallback func(key string, value interface{})

// Cache is a synchronized map of items that auto-expire once stale
type Cache struct {
	mutex                sync.RWMutex
	ttl                  time.Duration
	items                map[string]*Item
	expireCallback       ExpireCallback
	priorityQueue        priorityQueue
	priorityQueueNewItem chan bool
}

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {
	cache := &Cache{
		items: make(map[string]*Item),
	}
	cache.NewPriorityQueue()
	cache.startPriorityQueueProcessing()
	return cache
}

// Set is a thread-safe way to add new items to the map
func (cache *Cache) Set(key string, data interface{}) {
	cache.SetWithTTL(key, data, cache.ttl)
}

// SetWithTTL is a thread-safe way to add new items to the map with individual ttl
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) {
	item, exists := cache.getItem(key)

	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if exists {
		if (item.ttl > 0) && (ttl == 0) {
			cache.priorityQueue.remove(item)
		}
		item.data = data
		item.ttl = ttl
	} else {
		item = &Item{data: data, ttl: ttl, key: key}
		item.touch()
	}
	cache.items[key] = item

	if item.ttl > 0 {
		if exists {
			cache.priorityQueue.update(item)
		} else {
			cache.priorityQueue.add(item)
		}

		cache.priorityQueueNewItem <- true
	}
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (interface{}, bool) {
	item, exists := cache.getItem(key)
	if exists {
		return item.data, true
	}
	return nil, false
}

func (cache *Cache) getItem(key string) (*Item, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item, exists := cache.items[key]
	if !exists || item.expired() {
		return nil, false
	}
	item.touch()
	cache.priorityQueue.update(item)
	return item, exists
}

// Count returns the number of items in the cache
func (cache *Cache) Count() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return len(cache.items)
}

// SetDefaultTTL set the default timeout
func (cache *Cache) SetDefaultTTL(ttl time.Duration) {
	cache.ttl = ttl
}

// SetExpireCallback is used to save the callback that is called on key expire
func (cache *Cache) SetExpireCallback(callback ExpireCallback) {
	cache.expireCallback = callback
}
