package ttlcache

import (
	"time"

	"github.com/LopatkinEvgeniy/clock"
)

const (
	// ItemNotExpire Will avoid the item being expired by TTL, but can still be exired by callback etc.
	ItemNotExpire time.Duration = -1
	// ItemExpireWithGlobalTTL will use the global TTL when set.
	ItemExpireWithGlobalTTL time.Duration = 0
)

func newItem(key string, data interface{}, ttl time.Duration, c clock.Clock) *item {
	item := &item{
		data:  data,
		ttl:   ttl,
		key:   key,
		clock: c,
	}
	// since nobody is aware yet of this item, it's safe to touch without lock here
	item.touch()
	return item
}

type item struct {
	key        string
	data       interface{}
	ttl        time.Duration
	expireAt   time.Time
	queueIndex int
	clock      clock.Clock
}

// Reset the item expiration time
func (item *item) touch() {
	if item.ttl > 0 {
		item.expireAt = item.clock.Now().Add(item.ttl)
	}
}

// Verify if the item is expired
func (item *item) expired() bool {
	if item.ttl <= 0 {
		return false
	}
	return item.expireAt.Before(item.clock.Now())
}
