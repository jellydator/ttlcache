package ttlcache

import (
	"testing"
	"time"

	"github.com/LopatkinEvgeniy/clock"
	"github.com/stretchr/testify/assert"
)

func TestItemExpired(t *testing.T) {
	mockClock := clock.NewFakeClock()
	item := newItem("key", "value", (100 * time.Millisecond), mockClock)
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
	mockClock.Advance(200 * time.Millisecond)
	assert.Equal(t, item.expired(), true, "Expected item to be expired once time has passed")
}

func TestItemTouch(t *testing.T) {
	mockClock := clock.NewFakeClock()
	item := newItem("key", "value", (100 * time.Millisecond), mockClock)
	oldExpireAt := item.expireAt
	mockClock.Advance(50 * time.Millisecond)
	item.touch()
	assert.NotEqual(t, oldExpireAt, item.expireAt, "Expected dates to be different")
	mockClock.Advance(150 * time.Millisecond)
	assert.Equal(t, item.expired(), true, "Expected item to be expired")
	item.touch()
	mockClock.Advance(50 * time.Millisecond)
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
}

func TestItemWithoutExpiration(t *testing.T) {
	mockClock := clock.NewFakeClock()
	item := newItem("key", "value", ItemNotExpire, mockClock)
	mockClock.Advance(50 * time.Millisecond)
	assert.Equal(t, item.expired(), false, "Expected item to not be expired")
}
