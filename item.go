package ttlcache

import (
	"time"
)

const (
	// ItemNotExpire Will avoid the item being expired by TTL, but can
	// still be exired by callback etc.
	ItemNotExpire time.Duration = -1
	// ItemExpireWithGlobalTTL will use the global TTL when set.
	ItemExpireWithGlobalTTL time.Duration = 0
)

func newItem[K comparable, V any](key K, value V, ttl time.Duration) *item[K, V] {
	item := &item[K, V]{
		key:   key,
		value: value,
		ttl:   ttl,
	}
	// since nobody is aware yet of this item, it's safe to touch
	// without lock here
	item.touch()
	return item
}

type item[K comparable, V any] struct {
	key        K
	value      V
	ttl        time.Duration
	expireAt   time.Time
	queueIndex int
}

// Reset the item expiration time
func (item *item[K, V]) touch() {
	if item.ttl > 0 {
		item.expireAt = time.Now().Add(item.ttl)
	}
}

// Verify if the item is expired
func (item *item[K, V]) expired() bool {
	if item.ttl <= 0 {
		return false
	}
	return item.expireAt.Before(time.Now())
}
