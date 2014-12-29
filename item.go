package ttlcache

import (
	"sync"
	"time"
)

// Item represents a record in the cache map
type Item struct {
	sync.RWMutex
	data   interface{}
	ttl    time.Duration
	expire <-chan time.Time
}

// Reset the item expiration time
func (item *Item) touch() {
	item.Lock()
	item.expire = time.After(item.ttl)
	item.Unlock()
}

func initializeEmptyItemsTTL(cache *Cache) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	for _, item := range cache.items {
		if item.ttl == 0 {
			item.Lock()
			item.ttl = cache.ttl
			item.expire = time.After(item.ttl)
			item.Unlock()
		}
	}
}
