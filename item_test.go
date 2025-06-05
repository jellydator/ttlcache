package ttlcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewItem(t *testing.T) {
	t.Parallel()

	item := NewItem("key", 123, time.Hour, false)
	require.NotNil(t, item)
	assert.Equal(t, "key", item.key)
	assert.Equal(t, 123, item.value)
	assert.Equal(t, time.Hour, item.ttl)
	assert.Equal(t, int64(-1), item.version)
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
}

func Test_newItemWithOpts(t *testing.T) {
	t.Parallel()
	cc := map[string]struct {
		opts   []ItemOption[string, int]
		assert func(t *testing.T, item *Item[string, int])
	}{
		"Item without any options": {
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(CostItem[string, int]{Key: item.key, Value: item.value}))
			},
		},
		"Item with version tracking disabled": {
			opts: []ItemOption[string, int]{
				itemOptionFunc[string, int](func(i *Item[string, int]) {
					i.version = -1
				}),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(CostItem[string, int]{Key: item.key, Value: item.value}))
			},
		},
		"Item with version tracking explicitly enabled": {
			opts: []ItemOption[string, int]{
				itemOptionFunc[string, int](func(i *Item[string, int]) {
					i.version = 0
				}),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(0), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(CostItem[string, int]{Key: item.key, Value: item.value}))
			},
		},
		"Item with cost calculation": {
			opts: []ItemOption[string, int]{
				itemOptionFunc[string, int](func(i *Item[string, int]) {
					i.calculateCost = func(item CostItem[string, int]) uint64 { return 5 }
				}),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(5), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(5), item.calculateCost(CostItem[string, int]{Key: item.key, Value: item.value}))
			},
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			item := NewItemWithOpts("key", 123, time.Hour, c.opts...)
			require.NotNil(t, item)
			assert.Equal(t, "key", item.key)
			assert.Equal(t, 123, item.value)
			assert.Equal(t, time.Hour, item.ttl)
			assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			c.assert(t, item)
		})
	}
}

func Test_Item_update(t *testing.T) {
	t.Parallel()

	initialTTL := -1 * time.Hour
	newValue := "world"

	cc := map[string]struct {
		opts   []ItemOption[string, string]
		ttl    time.Duration
		assert func(t *testing.T, item *Item[string, string])
	}{
		"With expiration in an hour": {
			ttl: time.Hour,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, time.Hour, item.ttl)
				assert.Equal(t, int64(-1), item.version)
				assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			},
		},
		"With previous or default TTL": {
			ttl: PreviousOrDefaultTTL,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, initialTTL, item.ttl)
				assert.Equal(t, int64(-1), item.version)
			},
		},
		"With no TTL": {
			ttl: NoTTL,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, NoTTL, item.ttl)
				assert.Equal(t, int64(-1), item.version)
				assert.Zero(t, item.expiresAt)
			},
		},
		"With version tracking explicitly disabled": {
			opts: []ItemOption[string, string]{
				itemOptionFunc[string, string](func(i *Item[string, string]) {
					i.version = -1
				}),
			},
			ttl: time.Hour,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, time.Hour, item.ttl)
				assert.Equal(t, int64(-1), item.version)
				assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			},
		},
		"With version calculation and version tracking": {
			opts: []ItemOption[string, string]{
				itemOptionFunc[string, string](func(i *Item[string, string]) {
					i.calculateCost = func(item CostItem[string, string]) uint64 { return uint64(len(item.Value)) }
					i.version = 0
				}),
			},
			ttl: time.Hour,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(len(newValue)), item.cost)
				assert.Equal(t, time.Hour, item.ttl)
				assert.Equal(t, int64(1), item.version)
				assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			},
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			item := NewItemWithOpts("test", "hello", initialTTL, c.opts...)

			item.update(newValue, c.ttl)

			assert.Equal(t, newValue, item.value)
			c.assert(t, item)
		})
	}

}

func Test_Item_touch(t *testing.T) {
	t.Parallel()

	var item Item[string, string]
	item.touch()
	assert.Equal(t, int64(0), item.version)
	assert.Zero(t, item.expiresAt)

	item.ttl = time.Hour
	item.touch()
	assert.Equal(t, int64(0), item.version)
	assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
}

func Test_Item_IsExpired(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	item := Item[string, string]{
		key: "test",
	}

	assert.Equal(t, "test", item.Key())
}

func Test_Item_Value(t *testing.T) {
	t.Parallel()

	item := Item[string, string]{
		value: "test",
	}

	assert.Equal(t, "test", item.Value())
}

func Test_Item_TTL(t *testing.T) {
	t.Parallel()

	item := Item[string, string]{
		ttl: time.Hour,
	}

	assert.Equal(t, time.Hour, item.TTL())
}

func Test_Item_Cost(t *testing.T) {
	t.Parallel()

	item := Item[string, string]{
		cost: 50,
	}

	assert.Equal(t, uint64(50), item.Cost())
}

func Test_Item_ExpiresAt(t *testing.T) {
	t.Parallel()

	now := time.Now()
	item := Item[string, string]{
		expiresAt: now,
	}

	assert.Equal(t, now, item.ExpiresAt())
}

func Test_Item_Version(t *testing.T) {
	t.Parallel()

	item := Item[string, string]{version: 5}
	assert.Equal(t, int64(5), item.Version())
}
