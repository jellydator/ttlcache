package ttlcache

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/singleflight"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func Test_New(t *testing.T) {
	c := New[string, string](
		WithTTL[string, string](time.Hour),
		WithCapacity[string, string](1),
	)
	require.NotNil(t, c)
	assert.NotNil(t, c.stopCh)
	assert.NotNil(t, c.items.values)
	assert.NotNil(t, c.items.lru)
	assert.NotNil(t, c.items.expQueue)
	assert.NotNil(t, c.items.timerCh)
	assert.NotNil(t, c.events.insertion.fns)
	assert.NotNil(t, c.events.eviction.fns)
	assert.Equal(t, time.Hour, c.options.ttl)
	assert.Equal(t, uint64(1), c.options.capacity)
}

func Test_Cache_updateExpirations(t *testing.T) {
	oldExp, newExp := time.Now().Add(time.Hour), time.Now().Add(time.Minute)

	cc := map[string]struct {
		TimerChValue time.Duration
		Fresh        bool
		EmptyQueue   bool
		OldExpiresAt time.Time
		NewExpiresAt time.Time
		Result       time.Duration
	}{
		"Update with fresh item and zero new and non zero old expiresAt fields": {
			Fresh:        true,
			OldExpiresAt: oldExp,
		},
		"Update with non fresh item and zero new and non zero old expiresAt fields": {
			OldExpiresAt: oldExp,
		},
		"Update with fresh item and matching new and old expiresAt fields": {
			Fresh:        true,
			OldExpiresAt: oldExp,
			NewExpiresAt: oldExp,
		},
		"Update with non fresh item and matching new and old expiresAt fields": {
			OldExpiresAt: oldExp,
			NewExpiresAt: oldExp,
		},
		"Update with non zero new expiresAt field and empty queue": {
			Fresh:        true,
			EmptyQueue:   true,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with fresh item and non zero new and zero old expiresAt fields": {
			Fresh:        true,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with non fresh item and non zero new and zero old expiresAt fields": {
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with fresh item and non zero new and old expiresAt fields": {
			Fresh:        true,
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with non fresh item and non zero new and old expiresAt fields": {
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with full timerCh (lesser value), fresh item and non zero new and old expiresAt fields": {
			TimerChValue: time.Second,
			Fresh:        true,
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Second,
		},
		"Update with full timerCh (lesser value), non fresh item and non zero new and old expiresAt fields": {
			TimerChValue: time.Second,
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Second,
		},
		"Update with full timerCh (greater value), fresh item and non zero new and old expiresAt fields": {
			TimerChValue: time.Hour,
			Fresh:        true,
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
		"Update with full timerCh (greater value), non fresh item and non zero new and old expiresAt fields": {
			TimerChValue: time.Hour,
			OldExpiresAt: oldExp,
			NewExpiresAt: newExp,
			Result:       time.Until(newExp),
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			t.Parallel()

			cache := prepCache(time.Hour)

			if c.TimerChValue > 0 {
				cache.items.timerCh <- c.TimerChValue
			}

			elem := &list.Element{
				Value: &Item[string, string]{
					expiresAt: c.NewExpiresAt,
				},
			}

			if !c.EmptyQueue {
				cache.items.expQueue.push(&list.Element{
					Value: &Item[string, string]{
						expiresAt: c.OldExpiresAt,
					},
				})

				if !c.Fresh {
					elem = &list.Element{
						Value: &Item[string, string]{
							expiresAt: c.OldExpiresAt,
						},
					}
					cache.items.expQueue.push(elem)

					elem.Value.(*Item[string, string]).expiresAt = c.NewExpiresAt
				}
			}

			cache.updateExpirations(c.Fresh, elem)

			var res time.Duration

			select {
			case res = <-cache.items.timerCh:
			default:
			}

			assert.InDelta(t, c.Result, res, float64(time.Second))
		})
	}
}

func Test_Cache_set(t *testing.T) {
	const newKey, existingKey, evictedKey = "newKey123", "existingKey", "evicted"

	cc := map[string]struct {
		Capacity  uint64
		Key       string
		TTL       time.Duration
		Metrics   Metrics
		ExpectFns bool
	}{
		"Set with existing key and custom TTL": {
			Key: existingKey,
			TTL: time.Minute,
		},
		"Set with existing key and NoTTL": {
			Key: existingKey,
			TTL: NoTTL,
		},
		"Set with existing key and DefaultTTL": {
			Key: existingKey,
			TTL: DefaultTTL,
		},
		"Set with existing key and PreviousOrDefaultTTL": {
			Key: existingKey,
			TTL: PreviousOrDefaultTTL,
		},
		"Set with new key and eviction caused by small capacity": {
			Capacity: 3,
			Key:      newKey,
			TTL:      DefaultTTL,
			Metrics: Metrics{
				Insertions: 1,
				Evictions:  1,
			},
			ExpectFns: true,
		},
		"Set with new key and no eviction caused by large capacity": {
			Capacity: 10,
			Key:      newKey,
			TTL:      DefaultTTL,
			Metrics: Metrics{
				Insertions: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and custom TTL": {
			Key: newKey,
			TTL: time.Minute,
			Metrics: Metrics{
				Insertions: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and NoTTL": {
			Key: newKey,
			TTL: NoTTL,
			Metrics: Metrics{
				Insertions: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and DefaultTTL": {
			Key: newKey,
			TTL: DefaultTTL,
			Metrics: Metrics{
				Insertions: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and PreviousOrDefaultTTL": {
			Key: newKey,
			TTL: PreviousOrDefaultTTL,
			Metrics: Metrics{
				Insertions: 1,
			},
			ExpectFns: true,
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			t.Parallel()

			var (
				insertFnsCalls   int
				evictionFnsCalls int
			)

			// calculated based on how addToCache sets ttl
			existingKeyTTL := time.Hour + time.Minute

			cache := prepCache(time.Hour, evictedKey, existingKey, "test3")
			cache.options.capacity = c.Capacity
			cache.options.ttl = time.Minute * 20
			cache.events.insertion.fns[1] = func(item *Item[string, string]) {
				assert.Equal(t, newKey, item.key)
				insertFnsCalls++
			}
			cache.events.insertion.fns[2] = cache.events.insertion.fns[1]
			cache.events.eviction.fns[1] = func(r EvictionReason, item *Item[string, string]) {
				assert.Equal(t, EvictionReasonCapacityReached, r)
				assert.Equal(t, evictedKey, item.key)
				evictionFnsCalls++
			}
			cache.events.eviction.fns[2] = cache.events.eviction.fns[1]

			total := 3
			if c.Key == newKey && (c.Capacity == 0 || c.Capacity >= 4) {
				total++
			}

			item := cache.set(c.Key, "value123", c.TTL)

			if c.ExpectFns {
				assert.Equal(t, 2, insertFnsCalls)

				if c.Capacity > 0 && c.Capacity < 4 {
					assert.Equal(t, 2, evictionFnsCalls)
				}
			}

			assert.Same(t, cache.items.values[c.Key].Value.(*Item[string, string]), item)
			assert.Len(t, cache.items.values, total)
			assert.Equal(t, c.Key, item.key)
			assert.Equal(t, "value123", item.value)
			assert.Equal(t, c.Key, cache.items.lru.Front().Value.(*Item[string, string]).key)
			assert.Equal(t, c.Metrics, cache.metrics)

			if c.Capacity > 0 && c.Capacity < 4 {
				assert.NotEqual(t, evictedKey, cache.items.lru.Back().Value.(*Item[string, string]).key)
			}

			switch {
			case c.TTL == DefaultTTL:
				assert.Equal(t, cache.options.ttl, item.ttl)
				assert.WithinDuration(t, time.Now(), item.expiresAt, cache.options.ttl)
				assert.Equal(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			case c.TTL > DefaultTTL:
				assert.Equal(t, c.TTL, item.ttl)
				assert.WithinDuration(t, time.Now(), item.expiresAt, c.TTL)
				assert.Equal(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			case c.TTL == PreviousOrDefaultTTL:
				expectedTTL := cache.options.ttl
				if c.Key == existingKey {
					expectedTTL = existingKeyTTL
				}
				assert.Equal(t, expectedTTL, item.ttl)
				assert.WithinDuration(t, time.Now(), item.expiresAt, expectedTTL)
			default:
				assert.Equal(t, c.TTL, item.ttl)
				assert.Zero(t, item.expiresAt)
				assert.NotEqual(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			}
		})
	}

	// finally, test proper expiration queue handling on expired item update.
	// recreate situation when expired item gets updated
	// and not auto-cleaned up yet.
	c := New[string, struct{}](
		WithDisableTouchOnHit[string,struct{}](),
	)

	// insert an item and let it expire
	c.Set("test", struct{}{}, 1)
	time.Sleep(50*time.Millisecond)

	// update the expired item
	updatedItem := c.Set("test", struct{}{}, 100*time.Millisecond)

	// eviction should not happen as we prolonged element
	cl := c.OnEviction(func(_ context.Context, _ EvictionReason, item *Item[string, struct{}]){
		t.Errorf("eviction happened even though item has not expired yet: key=%s, evicted item expiresAt=%s, updated item expiresAt=%s, now=%s",
			item.Key(),
			item.ExpiresAt().String(),
			updatedItem.ExpiresAt().String(),
			time.Now().String())
	})
	// start of automatic cleanup process is delayed to allow update win the race
	// and update expired before its removal
	go c.Start()

	time.Sleep(90*time.Millisecond)
	cl()
	c.Stop()
}

func Test_Cache_get(t *testing.T) {
	const existingKey, notFoundKey, expiredKey = "existing", "notfound", "expired"

	cc := map[string]struct {
		Key     string
		Touch   bool
		WithTTL bool
	}{
		"Retrieval of non-existent item": {
			Key: notFoundKey,
		},
		"Retrieval of expired item": {
			Key: expiredKey,
		},
		"Retrieval of existing item without update": {
			Key: existingKey,
		},
		"Retrieval of existing item with touch and non zero TTL": {
			Key:     existingKey,
			Touch:   true,
			WithTTL: true,
		},
		"Retrieval of existing item with touch and zero TTL": {
			Key:   existingKey,
			Touch: true,
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			t.Parallel()

			cache := prepCache(time.Hour, existingKey, "test2", "test3")
			addToCache(cache, time.Nanosecond, expiredKey)
			time.Sleep(time.Millisecond) // force expiration

			oldItem := cache.items.values[existingKey].Value.(*Item[string, string])
			oldQueueIndex := oldItem.queueIndex
			oldExpiresAt := oldItem.expiresAt

			if c.WithTTL {
				oldItem.ttl = time.Hour * 30
			} else {
				oldItem.ttl = 0
			}

			elem := cache.get(c.Key, c.Touch)

			if c.Key == notFoundKey {
				assert.Nil(t, elem)
				return
			}

			if c.Key == expiredKey {
				assert.True(t, time.Now().After(cache.items.values[expiredKey].Value.(*Item[string, string]).expiresAt))
				assert.Nil(t, elem)
				return
			}

			require.NotNil(t, elem)
			item := elem.Value.(*Item[string, string])

			if c.Touch && c.WithTTL {
				assert.True(t, item.expiresAt.After(oldExpiresAt))
				assert.NotEqual(t, oldQueueIndex, item.queueIndex)
			} else {
				assert.True(t, item.expiresAt.Equal(oldExpiresAt))
				assert.Equal(t, oldQueueIndex, item.queueIndex)
			}

			assert.Equal(t, c.Key, cache.items.lru.Front().Value.(*Item[string, string]).key)
		})
	}
}

func Test_Cache_evict(t *testing.T) {
	var (
		key1FnsCalls int
		key2FnsCalls int
		key3FnsCalls int
		key4FnsCalls int
	)

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.eviction.fns[1] = func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		switch item.key {
		case "1":
			key1FnsCalls++
		case "2":
			key2FnsCalls++
		case "3":
			key3FnsCalls++
		case "4":
			key4FnsCalls++
		}
	}
	cache.events.eviction.fns[2] = cache.events.eviction.fns[1]

	// delete only specified
	cache.evict(EvictionReasonDeleted, cache.items.lru.Back(), cache.items.lru.Back().Prev())

	assert.Equal(t, 2, key1FnsCalls)
	assert.Equal(t, 2, key2FnsCalls)
	assert.Zero(t, key3FnsCalls)
	assert.Zero(t, key4FnsCalls)
	assert.Len(t, cache.items.values, 2)
	assert.NotContains(t, cache.items.values, "1")
	assert.NotContains(t, cache.items.values, "2")
	assert.Equal(t, uint64(2), cache.metrics.Evictions)

	// delete all
	key1FnsCalls, key2FnsCalls = 0, 0
	cache.metrics.Evictions = 0

	cache.evict(EvictionReasonDeleted)

	assert.Zero(t, key1FnsCalls)
	assert.Zero(t, key2FnsCalls)
	assert.Equal(t, 2, key3FnsCalls)
	assert.Equal(t, 2, key4FnsCalls)
	assert.Empty(t, cache.items.values)
	assert.NotContains(t, cache.items.values, "3")
	assert.NotContains(t, cache.items.values, "4")
	assert.Equal(t, uint64(2), cache.metrics.Evictions)
}

func Test_Cache_Set(t *testing.T) {
	cache := prepCache(time.Hour, "test1", "test2", "test3")
	item := cache.Set("hello", "value123", time.Minute)
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["hello"].Value)

	item = cache.Set("test1", "value123", time.Minute)
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["test1"].Value)
}

func Test_Cache_Get(t *testing.T) {
	const notFoundKey, foundKey = "notfound", "test1"
	cc := map[string]struct {
		Key            string
		DefaultOptions options[string, string]
		CallOptions    []Option[string, string]
		Metrics        Metrics
		Result         *Item[string, string]
	}{
		"Get without loader when item is not found": {
			Key: notFoundKey,
			Metrics: Metrics{
				Misses: 1,
			},
		},
		"Get with default loader that returns non nil value when item is not found": {
			Key: notFoundKey,
			DefaultOptions: options[string, string]{
				loader: LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return &Item[string, string]{key: "test"}
				}),
			},
			Metrics: Metrics{
				Misses: 1,
			},
			Result: &Item[string, string]{key: "test"},
		},
		"Get with default loader that returns nil value when item is not found": {
			Key: notFoundKey,
			DefaultOptions: options[string, string]{
				loader: LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return nil
				}),
			},
			Metrics: Metrics{
				Misses: 1,
			},
		},
		"Get with call loader that returns non nil value when item is not found": {
			Key: notFoundKey,
			DefaultOptions: options[string, string]{
				loader: LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return &Item[string, string]{key: "test"}
				}),
			},
			CallOptions: []Option[string, string]{
				WithLoader[string, string](LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return &Item[string, string]{key: "hello"}
				})),
			},
			Metrics: Metrics{
				Misses: 1,
			},
			Result: &Item[string, string]{key: "hello"},
		},
		"Get with call loader that returns nil value when item is not found": {
			Key: notFoundKey,
			DefaultOptions: options[string, string]{
				loader: LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return &Item[string, string]{key: "test"}
				}),
			},
			CallOptions: []Option[string, string]{
				WithLoader[string, string](LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
					return nil
				})),
			},
			Metrics: Metrics{
				Misses: 1,
			},
		},
		"Get when TTL extension is disabled by default and item is found": {
			Key: foundKey,
			DefaultOptions: options[string, string]{
				disableTouchOnHit: true,
			},
			Metrics: Metrics{
				Hits: 1,
			},
		},
		"Get when TTL extension is disabled and item is found": {
			Key: foundKey,
			CallOptions: []Option[string, string]{
				WithDisableTouchOnHit[string, string](),
			},
			Metrics: Metrics{
				Hits: 1,
			},
		},
		"Get when item is found": {
			Key: foundKey,
			Metrics: Metrics{
				Hits: 1,
			},
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			t.Parallel()

			cache := prepCache(time.Minute, foundKey, "test2", "test3")
			oldExpiresAt := cache.items.values[foundKey].Value.(*Item[string, string]).expiresAt
			cache.options = c.DefaultOptions

			res := cache.Get(c.Key, c.CallOptions...)

			if c.Key == foundKey {
				c.Result = cache.items.values[foundKey].Value.(*Item[string, string])
				assert.Equal(t, foundKey, cache.items.lru.Front().Value.(*Item[string, string]).key)
			}

			assert.Equal(t, c.Metrics, cache.metrics)

			if !assert.Equal(t, c.Result, res) || res == nil || res.ttl == 0 {
				return
			}

			applyOptions(&c.DefaultOptions, c.CallOptions...)

			if c.DefaultOptions.disableTouchOnHit {
				assert.Equal(t, oldExpiresAt, res.expiresAt)
				return
			}

			assert.True(t, oldExpiresAt.Before(res.expiresAt))
			assert.WithinDuration(t, time.Now(), res.expiresAt, res.ttl)
		})
	}
}

func Test_Cache_Delete(t *testing.T) {
	var fnsCalls int

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.eviction.fns[1] = func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		fnsCalls++
	}
	cache.events.eviction.fns[2] = cache.events.eviction.fns[1]

	// not found
	cache.Delete("1234")
	assert.Zero(t, fnsCalls)
	assert.Len(t, cache.items.values, 4)

	// success
	cache.Delete("1")
	assert.Equal(t, 2, fnsCalls)
	assert.Len(t, cache.items.values, 3)
	assert.NotContains(t, cache.items.values, "1")
}

func Test_Cache_Has(t *testing.T) {
	cc := map[string]struct {
		keys      []string
		searchKey string
		has       bool
	}{
		"Empty cache": {
			keys:      []string{},
			searchKey: "key1",
			has:       false,
		},
		"Key exists": {
			keys:      []string{"key1", "key2", "key3"},
			searchKey: "key2",
			has:       true,
		},
		"Key doesn't exist": {
			keys:      []string{"key1", "key2", "key3"},
			searchKey: "key4",
			has:       false,
		},
	}

	for name, tc := range cc {
		t.Run(name, func(t *testing.T) {
			c := prepCache(NoTTL, tc.keys...)
			has := c.Has(tc.searchKey)
			assert.Equal(t, tc.has, has)
		})
	}
}

func Test_Cache_GetOrSet(t *testing.T) {
	cache := prepCache(time.Hour)
	item, retrieved := cache.GetOrSet("test", "1", WithTTL[string, string](time.Minute))
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["test"].Value)
	assert.False(t, retrieved)

	item, retrieved = cache.GetOrSet("test", "1", WithTTL[string, string](time.Minute))
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["test"].Value)
	assert.True(t, retrieved)

	item, retrieved = cache.GetOrSet("test2", "1", WithTTL[string, string](time.Microsecond))
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["test2"].Value)
	assert.False(t, retrieved)

	time.Sleep(time.Millisecond)
	item, retrieved = cache.GetOrSet("test2", "2", WithTTL[string, string](time.Minute))
	require.NotNil(t, item)
	assert.Same(t, item, cache.items.values["test2"].Value)
	assert.False(t, retrieved)
}

func Test_Cache_GetAndDelete(t *testing.T) {
	cache := prepCache(time.Hour, "test1", "test2", "test3")
	listItem := cache.items.lru.Front()
	require.NotNil(t, listItem)
	assert.Same(t, listItem, cache.items.values["test3"])

	item, present := cache.GetAndDelete("test3")
	require.NotNil(t, item)
	assert.Nil(t, cache.items.values["test3"])
	assert.True(t, present)

	item, present = cache.GetAndDelete("test3")
	require.Nil(t, item)
	assert.Nil(t, cache.items.values["test3"])
	assert.False(t, present)

	loadedItem := &Item[string, string]{key: "test"}
	item, present = cache.GetAndDelete(
		"test3",
		WithLoader[string, string](
			LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] { return loadedItem }),
		),
	)
	require.NotNil(t, item)
	assert.Nil(t, cache.items.values["test3"])
	assert.True(t, present)
	assert.Same(t, item, loadedItem)
}

func Test_Cache_DeleteAll(t *testing.T) {
	var (
		key1FnsCalls int
		key2FnsCalls int
		key3FnsCalls int
		key4FnsCalls int
	)

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.eviction.fns[1] = func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		switch item.key {
		case "1":
			key1FnsCalls++
		case "2":
			key2FnsCalls++
		case "3":
			key3FnsCalls++
		case "4":
			key4FnsCalls++
		}
	}
	cache.events.eviction.fns[2] = cache.events.eviction.fns[1]

	cache.DeleteAll()
	assert.Empty(t, cache.items.values)
	assert.Equal(t, 2, key1FnsCalls)
	assert.Equal(t, 2, key2FnsCalls)
	assert.Equal(t, 2, key3FnsCalls)
	assert.Equal(t, 2, key4FnsCalls)
}

func Test_Cache_DeleteExpired(t *testing.T) {
	var (
		key1FnsCalls int
		key2FnsCalls int
	)

	cache := prepCache(time.Hour)
	cache.events.eviction.fns[1] = func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonExpired, r)
		switch item.key {
		case "5":
			key1FnsCalls++
		case "6":
			key2FnsCalls++
		}
	}
	cache.events.eviction.fns[2] = cache.events.eviction.fns[1]

	// one item
	addToCache(cache, time.Nanosecond, "5")

	cache.DeleteExpired()
	assert.Empty(t, cache.items.values)
	assert.NotContains(t, cache.items.values, "5")
	assert.Equal(t, 2, key1FnsCalls)

	key1FnsCalls = 0

	// empty
	cache.DeleteExpired()
	assert.Empty(t, cache.items.values)

	// non empty
	addToCache(cache, time.Hour, "1", "2", "3", "4")
	addToCache(cache, time.Nanosecond, "5")
	addToCache(cache, time.Nanosecond, "6") // we need multiple calls to avoid adding time.Minute to ttl
	time.Sleep(time.Millisecond)            // force expiration

	cache.DeleteExpired()
	assert.Len(t, cache.items.values, 4)
	assert.NotContains(t, cache.items.values, "5")
	assert.NotContains(t, cache.items.values, "6")
	assert.Equal(t, 2, key1FnsCalls)
	assert.Equal(t, 2, key2FnsCalls)
}

func Test_Cache_Touch(t *testing.T) {
	cache := prepCache(time.Hour, "1", "2")
	oldExpiresAt := cache.items.values["1"].Value.(*Item[string, string]).expiresAt

	cache.Touch("1")

	newExpiresAt := cache.items.values["1"].Value.(*Item[string, string]).expiresAt
	assert.True(t, newExpiresAt.After(oldExpiresAt))
	assert.Equal(t, "1", cache.items.lru.Front().Value.(*Item[string, string]).key)
}

func Test_Cache_Len(t *testing.T) {
	cache := prepCache(time.Hour, "1", "2")
	assert.Equal(t, 2, cache.Len())
}

func Test_Cache_Keys(t *testing.T) {
	cache := prepCache(time.Hour, "1", "2", "3")
	assert.ElementsMatch(t, []string{"1", "2", "3"}, cache.Keys())
}

func Test_Cache_Items(t *testing.T) {
	cache := prepCache(time.Hour, "1", "2", "3")
	items := cache.Items()
	require.Len(t, items, 3)

	require.Contains(t, items, "1")
	assert.Equal(t, "1", items["1"].key)
	require.Contains(t, items, "2")
	assert.Equal(t, "2", items["2"].key)
	require.Contains(t, items, "3")
	assert.Equal(t, "3", items["3"].key)
}

func Test_Cache_Range(t *testing.T) {
	c := prepCache(DefaultTTL, "1", "2", "3", "4", "5")
	var results []string

	c.Range(func(item *Item[string, string]) bool {
		results = append(results, item.Key())
		return item.Key() != "4"
	})

	assert.Equal(t, []string{"5", "4"}, results)

	emptyCache := New[string, string]()
	assert.NotPanics(t, func() {
		emptyCache.Range(func(item *Item[string, string]) bool {
			return false
		})
	})
}

func Test_Cache_Metrics(t *testing.T) {
	cache := Cache[string, string]{
		metrics: Metrics{Evictions: 10},
	}

	assert.Equal(t, Metrics{Evictions: 10}, cache.Metrics())
}

func Test_Cache_Start(t *testing.T) {
	cache := prepCache(0)
	cache.stopCh = make(chan struct{})

	addToCache(cache, time.Nanosecond, "1")
	time.Sleep(time.Millisecond) // force expiration

	fn := func(r EvictionReason, _ *Item[string, string]) {
		go func() {
			assert.Equal(t, EvictionReasonExpired, r)

			cache.metricsMu.RLock()
			v := cache.metrics.Evictions
			cache.metricsMu.RUnlock()

			switch v {
			case 1:
				cache.items.mu.Lock()
				addToCache(cache, time.Nanosecond, "2")
				cache.items.mu.Unlock()
				cache.options.ttl = time.Hour
				cache.items.timerCh <- time.Millisecond
			case 2:
				cache.items.mu.Lock()
				addToCache(cache, time.Second, "3")
				addToCache(cache, NoTTL, "4")
				cache.items.mu.Unlock()
				cache.items.timerCh <- time.Millisecond
			default:
				close(cache.stopCh)
			}
		}()
	}
	cache.events.eviction.fns[1] = fn

	cache.Start()
}

func Test_Cache_Stop(t *testing.T) {
	cache := Cache[string, string]{
		stopCh: make(chan struct{}, 1),
	}
	cache.Stop()
	assert.Len(t, cache.stopCh, 1)
}

func Test_Cache_OnInsertion(t *testing.T) {
	checkCh := make(chan struct{})
	resCh := make(chan struct{})
	cache := prepCache(time.Hour)
	del1 := cache.OnInsertion(func(_ context.Context, _ *Item[string, string]) {
		checkCh <- struct{}{}
	})
	del2 := cache.OnInsertion(func(_ context.Context, _ *Item[string, string]) {
		checkCh <- struct{}{}
	})

	require.Len(t, cache.events.insertion.fns, 2)
	assert.Equal(t, uint64(2), cache.events.insertion.nextID)

	cache.events.insertion.fns[0](nil)

	go func() {
		del1()
		resCh <- struct{}{}
	}()
	assert.Never(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		select {
		case <-checkCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)
	assert.Eventually(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)

	require.Len(t, cache.events.insertion.fns, 1)
	assert.NotContains(t, cache.events.insertion.fns, uint64(0))
	assert.Contains(t, cache.events.insertion.fns, uint64(1))

	cache.events.insertion.fns[1](nil)

	go func() {
		del2()
		resCh <- struct{}{}
	}()
	assert.Never(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		select {
		case <-checkCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)
	assert.Eventually(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)

	assert.Empty(t, cache.events.insertion.fns)
	assert.NotContains(t, cache.events.insertion.fns, uint64(1))
}

func Test_Cache_OnEviction(t *testing.T) {
	checkCh := make(chan struct{})
	resCh := make(chan struct{})
	cache := prepCache(time.Hour)
	del1 := cache.OnEviction(func(_ context.Context, _ EvictionReason, _ *Item[string, string]) {
		checkCh <- struct{}{}
	})
	del2 := cache.OnEviction(func(_ context.Context, _ EvictionReason, _ *Item[string, string]) {
		checkCh <- struct{}{}
	})

	require.Len(t, cache.events.eviction.fns, 2)
	assert.Equal(t, uint64(2), cache.events.eviction.nextID)

	cache.events.eviction.fns[0](0, nil)

	go func() {
		del1()
		resCh <- struct{}{}
	}()
	assert.Never(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		select {
		case <-checkCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)
	assert.Eventually(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)

	require.Len(t, cache.events.eviction.fns, 1)
	assert.NotContains(t, cache.events.eviction.fns, uint64(0))
	assert.Contains(t, cache.events.eviction.fns, uint64(1))

	cache.events.eviction.fns[1](0, nil)

	go func() {
		del2()
		resCh <- struct{}{}
	}()
	assert.Never(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Eventually(t, func() bool {
		select {
		case <-checkCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)
	assert.Eventually(t, func() bool {
		select {
		case <-resCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*500, time.Millisecond*250)

	assert.Empty(t, cache.events.eviction.fns)
	assert.NotContains(t, cache.events.eviction.fns, uint64(1))
}

func Test_LoaderFunc_Load(t *testing.T) {
	var called bool

	fn := LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
		called = true
		return nil
	})

	assert.Nil(t, fn(nil, ""))
	assert.True(t, called)
}

func Test_NewSuppressedLoader(t *testing.T) {
	var called bool

	loader := LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
		called = true
		return nil
	})

	// uses the provided loader and group parameters
	group := &singleflight.Group{}

	sl := NewSuppressedLoader[string, string](loader, group)
	require.NotNil(t, sl)
	require.NotNil(t, sl.loader)

	sl.loader.Load(nil, "")

	assert.True(t, called)
	assert.Equal(t, group, sl.group)

	// uses the provided loader and automatically creates a new instance
	// of *singleflight.Group as nil parameter is passed
	called = false

	sl = NewSuppressedLoader[string, string](loader, nil)
	require.NotNil(t, sl)
	require.NotNil(t, sl.loader)

	sl.loader.Load(nil, "")

	assert.True(t, called)
	assert.NotNil(t, group, sl.group)
}

func Test_SuppressedLoader_Load(t *testing.T) {
	var (
		mu        sync.Mutex
		loadCalls int
		releaseCh = make(chan struct{})
		res       *Item[string, string]
	)

	l := SuppressedLoader[string, string]{
		loader: LoaderFunc[string, string](func(_ *Cache[string, string], _ string) *Item[string, string] {
			mu.Lock()
			loadCalls++
			mu.Unlock()

			<-releaseCh

			if res == nil {
				return nil
			}

			res1 := *res

			return &res1
		}),
		group: &singleflight.Group{},
	}

	var (
		wg           sync.WaitGroup
		item1, item2 *Item[string, string]
	)

	cache := prepCache(time.Hour)

	// nil result
	wg.Add(2)

	go func() {
		item1 = l.Load(cache, "test")
		wg.Done()
	}()

	go func() {
		item2 = l.Load(cache, "test")
		wg.Done()
	}()

	time.Sleep(time.Millisecond * 100) // wait for goroutines to halt
	releaseCh <- struct{}{}

	wg.Wait()
	require.Nil(t, item1)
	require.Nil(t, item2)
	assert.Equal(t, 1, loadCalls)

	// non nil result
	res = &Item[string, string]{key: "test"}
	loadCalls = 0
	wg.Add(2)

	go func() {
		item1 = l.Load(cache, "test")
		wg.Done()
	}()

	go func() {
		item2 = l.Load(cache, "test")
		wg.Done()
	}()

	time.Sleep(time.Millisecond * 100) // wait for goroutines to halt
	releaseCh <- struct{}{}

	wg.Wait()
	require.Same(t, item1, item2)
	assert.Equal(t, "test", item1.key)
	assert.Equal(t, 1, loadCalls)
}

func prepCache(ttl time.Duration, keys ...string) *Cache[string, string] {
	c := &Cache[string, string]{}
	c.options.ttl = ttl
	c.items.values = make(map[string]*list.Element)
	c.items.lru = list.New()
	c.items.expQueue = newExpirationQueue[string, string]()
	c.items.timerCh = make(chan time.Duration, 1)
	c.events.eviction.fns = make(map[uint64]func(EvictionReason, *Item[string, string]))
	c.events.insertion.fns = make(map[uint64]func(*Item[string, string]))

	addToCache(c, ttl, keys...)

	return c
}

func addToCache(c *Cache[string, string], ttl time.Duration, keys ...string) {
	for i, key := range keys {
		item := newItem(
			key,
			fmt.Sprint("value of", key),
			ttl+time.Duration(i)*time.Minute,
			false,
		)
		elem := c.items.lru.PushFront(item)
		c.items.values[key] = elem
		c.items.expQueue.push(elem)
	}
}
