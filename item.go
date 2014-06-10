package ttlcache

import "time"

type Item struct {
	data    string
	expires *time.Time
}

func (item *Item) Touch(duration time.Duration) {
	expiration := time.Now().Add(duration)
	item.expires = &expiration
}

func (item *Item) Expired() bool {
	if item.expires == nil {
		return false
	}
	return item.expires.Before(time.Now())
}
