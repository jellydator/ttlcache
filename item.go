package ttlcache

import "time"

// Item represents a record in the cache map
type Item struct {
	data    string
	expires *time.Time
}

func (item *Item) touch(duration time.Duration) {
	expiration := time.Now().Add(duration)
	item.expires = &expiration
}

func (item *Item) expired() bool {
	if item.expires == nil {
		return false
	}
	return item.expires.Before(time.Now())
}
