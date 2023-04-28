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

func Test_Item_update(t *testing.T) {
	item := Item[string, string]{
		expiresAt: time.Now().Add(-time.Hour),
		value:     "hello",
	}

	item.update("test", time.Hour)
	assert.Equal(t, "test", item.value)
	assert.Equal(t, time.Hour, item.ttl)
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)

	item.update("hi", NoTTL)
	assert.Equal(t, "hi", item.value)
	assert.Equal(t, NoTTL, item.ttl)
	assert.Zero(t, item.expiresAt)
}

func Test_Item_touch(t *testing.T) {
	var item Item[string, string]
	item.touch()
	assert.Zero(t, item.expiresAt)

	item.ttl = time.Hour
	item.touch()
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
}

func Test_Item_IsExpired(t *testing.T) {
	// no ttl
	item := Item[string, string]{
		expiresAt: time.Now().Add(-time.Hour),
	}

	assert.False(t, item.IsExpired())

	// expired
	item.ttl = time.Hour
	assert.True(t, item.IsExpired())

	// not expired
	item.expiresAt = time.Now().Add(time.Hour)
	assert.False(t, item.IsExpired())
}

func Test_Item_Key(t *testing.T) {
	item := Item[string, string]{
		key: "test",
	}

	assert.Equal(t, "test", item.Key())
}

func Test_Item_Value(t *testing.T) {
	item := Item[string, string]{
		value: "test",
	}

	assert.Equal(t, "test", item.Value())
}

func Test_Item_TTL(t *testing.T) {
	item := Item[string, string]{
		ttl: time.Hour,
	}

	assert.Equal(t, time.Hour, item.TTL())
}

func Test_Item_ExpiresAt(t *testing.T) {
	now := time.Now()
	item := Item[string, string]{
		expiresAt: now,
	}

	assert.Equal(t, now, item.ExpiresAt())
}

func TestItem_Version(t *testing.T) {
	// TTL=DefaultTTL
	item := newItem("key1", "value1", DefaultTTL)
	assert.Equal(t, uint64(0), item.Version())

	item.update("newValue1", DefaultTTL)
	assert.Equal(t, uint64(1), item.Version())

	item.update("newValue2", DefaultTTL)
	assert.Equal(t, uint64(2), item.Version())

	item.touch()
	assert.Equal(t, uint64(2), item.Version())

	item.update("newValue2", time.Minute)
	item.touch()
	assert.Equal(t, uint64(4), item.Version())

	// TTL=time.Minute
	item2 := newItem("key1", "v1", time.Minute)
	assert.Equal(t, uint64(1), item2.Version())

	item2.update("v2", time.Minute)
	assert.Equal(t, uint64(2), item2.Version())

	item2.touch()
	assert.Equal(t, uint64(3), item2.Version())
}
