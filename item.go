package ttlcache

import (
	"time"
)

const (
	// NoExpiration indicates that an item should never expire. 
	NoExpiration time.Duration = -1

	// DefaultExpiration indicates that the default expiration
	// value should be used.
	DefaultExpiration time.Duration = 0
)

// item holds all the information that is associated with a single
// cache value. 
type item[K comparable, V any] struct {
	key        K
	value      V
	ttl        time.Duration
	expiresAt   time.Time
	queueIndex int
}

// newItem creates a new cache item.
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

// touch updates the item's expiration timestamp.
func (item *item[K, V]) touch() {
	if item.ttl <= 0 {
		return
	}

	item.expiresAt = time.Now().Add(item.ttl)
}

// isExpired returns a bool value that indicates whether the
// the item is expired or not.
func (item *item[K, V]) isExpired() bool {
	if item.ttl <= 0 {
		return false
	}

	return item.expiresAt.Before(time.Now())
}
