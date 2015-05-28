package ttlcache

import (
	"sync"
	"time"
)

const (
	ItemNotExpire           time.Duration = -1
	ItemExpireWithGlobalTTL time.Duration = 0
)

func newItem(key string, data interface{}, ttl time.Duration) *item {
	item := &item{
		data: data,
		ttl:  ttl,
		key:  key,
	}
	item.touch()
	return item
}

type item struct {
	key        string
	data       interface{}
	ttl        time.Duration
	expireAt   time.Time
	mutex      sync.Mutex
	queueIndex int
}

// Reset the item expiration time
func (item *item) touch() {
	item.mutex.Lock()
	if item.ttl > 0 {
		item.expireAt = time.Now().Add(item.ttl)
	}
	item.mutex.Unlock()
}

// Verify if the item is expired
func (item *item) expired() bool {
	item.mutex.Lock()
	if item.ttl <= 0 {
		item.mutex.Unlock()
		return false
	}
	expired := item.expireAt.Before(time.Now())
	item.mutex.Unlock()
	return expired
}
