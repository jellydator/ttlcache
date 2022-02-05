package ttlcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newItem(t *testing.T) {
	item := newItem("key", 123, time.Hour)
	require.NotNil(t, item)
	assert.Equal(t, "key", item.key)
	assert.Equal(t, 123, item.value)
	assert.Equal(t, time.Hour, item.ttl)
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
}

func Test_Item_touch(t *testing.T) {
	var item Item[string, string]
	item.touch()
	assert.Zero(t, item.expiresAt)

	item.ttl = time.Hour
	item.touch()
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
}

func Test_Item_isExpired(t *testing.T) {
	// no ttl
	item := Item[string, string]{
		expiresAt: time.Now().Add(-time.Hour),
	}

	assert.False(t, item.isExpired())

	// expired
	item.ttl = time.Hour
	assert.True(t, item.isExpired())

	// not expired
	item.expiresAt = time.Now().Add(time.Hour)
	assert.False(t, item.isExpired())
}
