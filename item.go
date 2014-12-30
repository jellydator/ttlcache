package ttlcache

import (
	"sync"
	"time"
)

// Item represents a record in the cache map
type Item struct {
	sync.RWMutex
	data    interface{}
	ttl     *time.Duration
	expires *time.Time
}

// Reset the item expiration time
func (item *Item) touch() {
	item.Lock()
	expiration := time.Now().Add(*item.ttl)
	item.expires = &expiration
	item.Unlock()
}

// Verify if the item is expired
func (item *Item) expired() bool {
	item.RLock()
	defer item.RUnlock()
	if item.expires == nil {
		return true
	} else {
		return item.expires.Before(time.Now())
	}
}
