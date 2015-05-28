package ttlcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestItemExpired(t *testing.T) {
	item := newItem("key", "value", (time.Duration(100) * time.Millisecond))
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
	<-time.After(200 * time.Millisecond)
	assert.Equal(t, item.expired(), true, "Expected item to be expired once time has passed")
}

func TestItemTouch(t *testing.T) {
	item := newItem("key", "value", (time.Duration(100) * time.Millisecond))
	oldExpireAt := item.expireAt
	<-time.After(50 * time.Millisecond)
	item.touch()
	assert.NotEqual(t, oldExpireAt, item.expireAt, "Expected dates to be different")
	<-time.After(150 * time.Millisecond)
	assert.Equal(t, item.expired(), true, "Expected item to be expired")
	item.touch()
	<-time.After(50 * time.Millisecond)
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
}

func TestItemWithoutExpiration(t *testing.T) {
	item := newItem("key", "value", ItemNotExpire)
	<-time.After(50 * time.Millisecond)
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
}
