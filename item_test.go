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

	for _, tc := range []struct {
		uc     string
		opts   []itemOption[string, int]
		assert func(t *testing.T, item *Item[string, int])
	}{
		{
			uc: "item without any options",
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(item))
			},
		},
		{
			uc: "item with version tracking disabled",
			opts: []itemOption[string, int]{
				withVersionTracking[string, int](false),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(item))
			},
		},
		{
			uc: "item with version tracking explicitly enabled",
			opts: []itemOption[string, int]{
				withVersionTracking[string, int](true),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(0), item.version)
				assert.Equal(t, uint64(0), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(0), item.calculateCost(item))
			},
		},
		{
			uc: "item with cost calculation",
			opts: []itemOption[string, int]{
				withCostFunc[string, int](func(item *Item[string, int]) uint64 { return 5 }),
			},
			assert: func(t *testing.T, item *Item[string, int]) {
				assert.Equal(t, int64(-1), item.version)
				assert.Equal(t, uint64(5), item.cost)
				require.NotNil(t, item.calculateCost)
				assert.Equal(t, uint64(5), item.calculateCost(item))
			},
		},
	} {
		t.Run(tc.uc, func(t *testing.T) {
			item := newItemWithOpts("key", 123, time.Hour, tc.opts...)
			require.NotNil(t, item)
			assert.Equal(t, "key", item.key)
			assert.Equal(t, 123, item.value)
			assert.Equal(t, time.Hour, item.ttl)
			assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			tc.assert(t, item)
		})
	}
}

func Test_Item_update(t *testing.T) {
	t.Parallel()

	initialTTL := -1 * time.Hour
	newValue := "world"

	for _, tc := range []struct {
		uc     string
		opts   []itemOption[string, string]
		ttl    time.Duration
		assert func(t *testing.T, item *Item[string, string])
	}{
		{
			uc:  "with expiration in an hour",
			ttl: time.Hour,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, time.Hour, item.ttl)
				assert.Equal(t, int64(-1), item.version)
				assert.WithinDuration(t, time.Now().Add(time.Hour), item.expiresAt, time.Minute)
			},
		},
		{
			uc:  "with previous or default ttl",
			ttl: PreviousOrDefaultTTL,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, initialTTL, item.ttl)
				assert.Equal(t, int64(-1), item.version)
			},
		},
		{
			uc:  "with no ttl",
			ttl: NoTTL,
			assert: func(t *testing.T, item *Item[string, string]) {
				t.Helper()

				assert.Equal(t, uint64(0), item.cost)
				assert.Equal(t, NoTTL, item.ttl)
				assert.Equal(t, int64(-1), item.version)
				assert.Zero(t, item.expiresAt)
			},
		},
		{
			uc: "with version tracking explicitly disabled",
			opts: []itemOption[string, string]{
				withVersionTracking[string, string](false),
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
		{
			uc: "with version calculation and version tracking",
			opts: []itemOption[string, string]{
				withVersionTracking[string, string](true),
				withCostFunc[string, string](func(item *Item[string, string]) uint64 { return uint64(len(item.value)) }),
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
	} {
		t.Run(tc.uc, func(t *testing.T) {
			item := newItemWithOpts[string, string]("test", "hello", initialTTL, tc.opts...)

			item.update(newValue, tc.ttl)

			assert.Equal(t, newValue, item.value)
			tc.assert(t, item)
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
