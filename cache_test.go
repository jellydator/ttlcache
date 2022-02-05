package ttlcache

import (
	"container/list"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_New(t *testing.T) {
	c := New[string, string]()
	require.NotNil(t, c)
	assert.NotNil(t, c.loaderGroup)
	assert.NotNil(t, c.stopCh)
	assert.NotNil(t, c.items.values)
	assert.NotNil(t, c.items.lru)
	assert.NotNil(t, c.items.expQueue)
	assert.NotNil(t, c.items.timerCh)
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
		"Set with new key and eviction caused by small capacity": {
			Capacity: 3,
			Key:      newKey,
			TTL:      DefaultTTL,
			Metrics: Metrics{
				Inserts:   1,
				Evictions: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and no eviction caused by large capacity": {
			Capacity: 10,
			Key:      newKey,
			TTL:      DefaultTTL,
			Metrics: Metrics{
				Inserts: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and custom TTL": {
			Key: newKey,
			TTL: time.Minute,
			Metrics: Metrics{
				Inserts: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and NoTTL": {
			Key: newKey,
			TTL: NoTTL,
			Metrics: Metrics{
				Inserts: 1,
			},
			ExpectFns: true,
		},
		"Set with new key and DefaultTTL": {
			Key: newKey,
			TTL: DefaultTTL,
			Metrics: Metrics{
				Inserts: 1,
			},
			ExpectFns: true,
		},
	}

	for cn, c := range cc {
		c := c

		t.Run(cn, func(t *testing.T) {
			t.Parallel()

			var (
				wg               sync.WaitGroup
				insertFnsMu      sync.Mutex
				insertFnsCalls   int
				evictionFnsMu    sync.Mutex
				evictionFnsCalls int
			)

			cache := prepCache(time.Hour, evictedKey, existingKey, "test3")
			cache.capacity = c.Capacity
			cache.defaultTTL = time.Minute * 20
			cache.events.insertFns = append(cache.events.insertFns, func(item *Item[string, string]) {
				assert.Equal(t, newKey, item.key)
				insertFnsMu.Lock()
				insertFnsCalls++
				insertFnsMu.Unlock()
				wg.Done()
			})
			cache.events.insertFns = append(cache.events.insertFns, cache.events.insertFns[0])
			cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, item *Item[string, string]) {
				assert.Equal(t, EvictionReasonCapacityReached, r)
				assert.Equal(t, evictedKey, item.key)
				evictionFnsMu.Lock()
				evictionFnsCalls++
				evictionFnsMu.Unlock()
				wg.Done()
			})
			cache.events.evictionFns = append(cache.events.evictionFns, cache.events.evictionFns[0])

			if c.ExpectFns {
				wg.Add(2)
			}

			total := 3
			if c.Key == newKey {
				if c.Capacity > 0 && c.Capacity < 4 {
					wg.Add(2)
				} else {
					total++
				}
			}

			item := cache.set(c.Key, "value123", c.TTL)

			if c.ExpectFns {
				wg.Wait()
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
				assert.Equal(t, cache.defaultTTL, item.ttl)
				assert.WithinDuration(t, time.Now(), item.expiresAt, cache.defaultTTL)
				assert.Equal(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			case c.TTL > DefaultTTL:
				assert.Equal(t, c.TTL, item.ttl)
				assert.WithinDuration(t, time.Now(), item.expiresAt, c.TTL)
				assert.Equal(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			default:
				assert.Equal(t, c.TTL, item.ttl)
				assert.Zero(t, item.expiresAt)
				assert.NotEqual(t, c.Key, cache.items.expQueue[0].Value.(*Item[string, string]).key)
			}
		})
	}
}

func Test_Cache_get(t *testing.T) {
	const existingKey, notFoundKey, expiredKey = "existing", "notfound", "expired"

	cc := map[string]struct {
		Key     string
		Update  bool
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
		"Retrieval of existing item with update and non zero TTL": {
			Key:     existingKey,
			Update:  true,
			WithTTL: true,
		},
		"Retrieval of existing item with update and zero TTL": {
			Key:    existingKey,
			Update: true,
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

			elem := cache.get(c.Key, c.Update)

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

			if c.Update && c.WithTTL {
				assert.True(t, item.expiresAt.After(oldExpiresAt))
				assert.NotEqual(t, oldQueueIndex, item.queueIndex)
			} else {
				assert.True(t, item.expiresAt.Equal(oldExpiresAt))
				assert.Equal(t, oldQueueIndex, item.queueIndex)
			}

			if c.Update {
				assert.Equal(t, c.Key, cache.items.lru.Front().Value.(*Item[string, string]).key)
			} else {
				assert.NotEqual(t, c.Key, cache.items.lru.Front().Value.(*Item[string, string]).key)
			}
		})
	}
}

func Test_Cache_evict(t *testing.T) {
	var (
		wg           sync.WaitGroup
		fnsMu        sync.Mutex
		key1FnsCalls int
		key2FnsCalls int
		key3FnsCalls int
		key4FnsCalls int
	)

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		fnsMu.Lock()
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
		fnsMu.Unlock()
		wg.Done()
	})
	cache.events.evictionFns = append(cache.events.evictionFns, cache.events.evictionFns[0])

	// delete only specified
	wg.Add(4)
	cache.evict(EvictionReasonDeleted, cache.items.lru.Back(), cache.items.lru.Back().Prev())
	wg.Wait()

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

	wg.Add(4)
	cache.evict(EvictionReasonDeleted)
	wg.Wait()

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
	//cache := prepCache(time.Hour, "test1", "test2", "test3")
}

func Test_Cache_Delete(t *testing.T) {
	var (
		wg       sync.WaitGroup
		fnsMu    sync.Mutex
		fnsCalls int
	)

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		fnsMu.Lock()
		fnsCalls++
		fnsMu.Unlock()
		wg.Done()
	})
	cache.events.evictionFns = append(cache.events.evictionFns, cache.events.evictionFns[0])

	// not found
	cache.Delete("1234")
	assert.Zero(t, fnsCalls)
	assert.Len(t, cache.items.values, 4)

	// success
	wg.Add(2)
	cache.Delete("1")
	wg.Wait()

	assert.Equal(t, 2, fnsCalls)
	assert.Len(t, cache.items.values, 3)
	assert.NotContains(t, cache.items.values, "1")
}

func Test_Cache_DeleteAll(t *testing.T) {
	var (
		wg           sync.WaitGroup
		fnsMu        sync.Mutex
		key1FnsCalls int
		key2FnsCalls int
		key3FnsCalls int
		key4FnsCalls int
	)

	cache := prepCache(time.Hour, "1", "2", "3", "4")
	cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonDeleted, r)
		fnsMu.Lock()
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
		fnsMu.Unlock()
		wg.Done()
	})
	cache.events.evictionFns = append(cache.events.evictionFns, cache.events.evictionFns[0])

	wg.Add(8)
	cache.DeleteAll()
	wg.Wait()

	assert.Empty(t, cache.items.values)
	assert.Equal(t, 2, key1FnsCalls)
	assert.Equal(t, 2, key2FnsCalls)
	assert.Equal(t, 2, key3FnsCalls)
	assert.Equal(t, 2, key4FnsCalls)
}

func Test_Cache_DeleteExpired(t *testing.T) {
	var (
		wg           sync.WaitGroup
		fnsMu        sync.Mutex
		key1FnsCalls int
		key2FnsCalls int
	)

	cache := prepCache(time.Hour)
	cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, item *Item[string, string]) {
		assert.Equal(t, EvictionReasonExpired, r)
		fnsMu.Lock()
		switch item.key {
		case "5":
			key1FnsCalls++
		case "6":
			key2FnsCalls++
		}
		fnsMu.Unlock()
		wg.Done()
	})
	cache.events.evictionFns = append(cache.events.evictionFns, cache.events.evictionFns[0])

	// one item
	addToCache(cache, time.Nanosecond, "5")

	wg.Add(2)
	cache.DeleteExpired()
	wg.Wait()

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

	wg.Add(4)
	cache.DeleteExpired()
	wg.Wait()

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

	cache.events.evictionFns = append(cache.events.evictionFns, func(r EvictionReason, _ *Item[string, string]) {
		assert.Equal(t, EvictionReasonExpired, r)

		cache.metricsMu.RLock()
		v := cache.metrics.Evictions
		cache.metricsMu.RUnlock()

		switch v {
		case 1:
			cache.items.mu.Lock()
			addToCache(cache, time.Nanosecond, "2")
			cache.items.mu.Unlock()
			cache.defaultTTL = time.Hour
			cache.items.timerCh <- time.Millisecond
		case 2:
			cache.items.mu.Lock()
			addToCache(cache, time.Second*2, "3")
			addToCache(cache, NoTTL, "4")
			cache.items.mu.Unlock()
			cache.items.timerCh <- time.Millisecond
		default:
			close(cache.stopCh)
		}
	})

	cache.Start()
}

func Test_Cache_Stop(t *testing.T) {
	cache := Cache[string, string]{
		stopCh: make(chan struct{}, 1),
	}
	cache.Stop()
	assert.Len(t, cache.stopCh, 1)
}

func prepCache(ttl time.Duration, keys ...string) *Cache[string, string] {
	c := &Cache[string, string]{defaultTTL: ttl}
	c.items.values = make(map[string]*list.Element)
	c.items.lru = list.New()
	c.items.expQueue = newExpirationQueue[string, string]()
	c.items.timerCh = make(chan time.Duration, 1)

	addToCache(c, ttl, keys...)

	return c
}

func addToCache(c *Cache[string, string], ttl time.Duration, keys ...string) {
	for i, key := range keys {
		item := newItem(
			key,
			fmt.Sprint("value of", key),
			ttl+time.Duration(i)*time.Minute,
		)
		elem := c.items.lru.PushFront(item)
		c.items.values[key] = elem
		c.items.expQueue.push(elem)
	}
}
