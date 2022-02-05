package ttlcache

import (
	"time"
)

const (
	// NoTTL indicates that an item should never expire.
	NoTTL time.Duration = -1

	// DefaultTTL indicates that the default TTL
	// value should be used.
	DefaultTTL time.Duration = 0
)

// Item holds all the information that is associated with a single
// cache value.
type Item[K comparable, V any] struct {
	key        K
	value      V
	ttl        time.Duration
	expiresAt  time.Time
	queueIndex int
}

// newItem creates a new cache item.
func newItem[K comparable, V any](key K, value V, ttl time.Duration) *Item[K, V] {
	item := &Item[K, V]{
		key:   key,
		value: value,
		ttl:   ttl,
	}

	// since nobody is aware of this item yet, it's safe to touch
	// without lock here
	item.touch()

	return item
}

// touch updates the item's expiration timestamp.
func (item *Item[K, V]) touch() {
	if item.ttl <= 0 {
		return
	}

	item.expiresAt = time.Now().Add(item.ttl)
}

// isExpired returns a bool value that indicates whether the
// the item is expired or not.
func (item *Item[K, V]) isExpired() bool {
	if item.ttl <= 0 {
		return false
	}

	return item.expiresAt.Before(time.Now())
}
