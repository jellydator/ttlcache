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
func (item *Item) touch(duration time.Duration) {
	item.Lock()
	item.expireCallback = time.After(duration)
	item.Unlock()
}
