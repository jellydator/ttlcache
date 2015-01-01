package ttlcache

import (
	"sync"
	"time"
)

// Item represents a record in the cache map
type Item struct {
	sync.RWMutex
	data    interface{}
	ttl     time.Duration
	expires time.Time
	index   int
	key     string
}

// Reset the item expiration time
func (item *Item) touch() {
	if item.ttl > 0 {
		item.Lock()
		defer item.Unlock()
		item.expires = time.Now().Add(item.ttl)
	}
}

// Verify if the item is expired
func (item *Item) expired() bool {
	item.RLock()
	defer item.RUnlock()
	if item.ttl == 0 {
		return false
	}
	return item.expires.Before(time.Now())
}
