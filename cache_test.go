package ttlcache_test

import (
	"math/rand"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"fmt"
	"sync"

	. "github.com/jellydator/ttlcache/v2"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// The SimpleCache interface enables quick-start.
func TestCache_SimpleCache(t *testing.T) {
	t.Parallel()
	var cache SimpleCache = NewCache()

	cache.SetTTL(time.Second)
	cache.Set("k", "v")
	cache.Get("k")
	cache.Purge()
	cache.Close()

}

// Issue 45 : This test was used to test different code paths for best performance.
func TestCache_GetByLoaderRace(t *testing.T) {
	t.Skip()
	t.Parallel()
	cache := NewCache()
	cache.SetTTL(time.Microsecond)
	defer cache.Close()

	loaderInvocations := uint64(0)
	inFlight := uint64(0)

	globalLoader := func(key string) (data interface{}, ttl time.Duration, err error) {
		atomic.AddUint64(&inFlight, 1)
		atomic.AddUint64(&loaderInvocations, 1)
		time.Sleep(time.Microsecond)
		assert.Equal(t, uint64(1), inFlight)
		defer atomic.AddUint64(&inFlight, ^uint64(0))
		return "global", 0, nil

	}
	cache.SetLoaderFunction(globalLoader)

	for i := 0; i < 1000; i++ {
		wg := sync.WaitGroup{}
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				key, _ := cache.Get("test")
				assert.Equal(t, "global", key)
				wg.Done()

			}()
		}
		wg.Wait()
		t.Logf("Invocations: %d\n", loaderInvocations)
		loaderInvocations = 0
	}

}

// Issue / PR #39: add customer loader function for each Get() #
// some middleware prefers to define specific context's etc per Get.
// This is faciliated by supplying a loder function with Get's.
func TestCache_GetByLoader(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	globalLoader := func(key string) (data interface{}, ttl time.Duration, err error) {
		return "global", 0, nil
	}
	cache.SetLoaderFunction(globalLoader)

	localLoader := func(key string) (data interface{}, ttl time.Duration, err error) {
		return "local", 0, nil
	}

	key, _ := cache.Get("test")
	assert.Equal(t, "global", key)

	cache.Remove("test")

	localKey, _ := cache.GetByLoader("test", localLoader)
	assert.Equal(t, "local", localKey)

	cache.Remove("test")

	globalKey, _ := cache.GetByLoader("test", globalLoader)
	assert.Equal(t, "global", globalKey)

	cache.Remove("test")

	defaultKey, _ := cache.GetByLoader("test", nil)
	assert.Equal(t, "global", defaultKey)

	cache.Remove("test")
}

func TestCache_GetByLoaderWithTtl(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	globalTtl := time.Duration(time.Minute)
	globalLoader := func(key string) (data interface{}, ttl time.Duration, err error) {
		return "global", globalTtl, nil
	}
	cache.SetLoaderFunction(globalLoader)

	localTtl := time.Duration(time.Hour)
	localLoader := func(key string) (data interface{}, ttl time.Duration, err error) {
		return "local", localTtl, nil
	}

	key, ttl, _ := cache.GetWithTTL("test")
	assert.Equal(t, "global", key)
	assert.Equal(t, ttl, globalTtl)
	cache.Remove("test")

	localKey, ttl2, _ := cache.GetByLoaderWithTtl("test", localLoader)
	assert.Equal(t, "local", localKey)
	assert.Equal(t, ttl2, localTtl)
	cache.Remove("test")

	globalKey, ttl3, _ := cache.GetByLoaderWithTtl("test", globalLoader)
	assert.Equal(t, "global", globalKey)
	assert.Equal(t, ttl3, globalTtl)
	cache.Remove("test")

	defaultKey, ttl4, _ := cache.GetByLoaderWithTtl("test", nil)
	assert.Equal(t, "global", defaultKey)
	assert.Equal(t, ttl4, globalTtl)
	cache.Remove("test")
}

// Issue #38: Feature request: ability to know why an expiry has occurred
func TestCache_textExpirationReasons(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	var reason EvictionReason
	var sync = make(chan struct{})
	expirationReason := func(key string, evReason EvictionReason, value interface{}) {
		reason = evReason
		sync <- struct{}{}
	}
	cache.SetExpirationReasonCallback(expirationReason)

	cache.SetTTL(time.Millisecond)
	cache.Set("one", "one")
	<-sync
	assert.Equal(t, Expired, reason)

	cache.SetTTL(time.Hour)
	cache.SetCacheSizeLimit(1)
	cache.Set("two", "two")
	cache.Set("twoB", "twoB")
	<-sync
	assert.Equal(t, EvictedSize, reason)

	cache.Remove("twoB")
	<-sync
	assert.Equal(t, Removed, reason)

	cache.SetTTL(time.Hour)
	cache.Set("three", "three")
	cache.Close()
	<-sync
	assert.Equal(t, Closed, reason)

}

func TestCache_TestTouch(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	lock := sync.Mutex{}

	lock.Lock()
	expired := false
	lock.Unlock()

	cache.SkipTTLExtensionOnHit(true)
	cache.SetExpirationCallback(func(key string, value interface{}) {
		lock.Lock()
		defer lock.Unlock()
		expired = true
	})

	cache.SetWithTTL("key", "data", time.Millisecond*900)
	<-time.After(time.Millisecond * 500)

	// no Touch
	//	cache.Touch("key")

	<-time.After(time.Millisecond * 500)
	lock.Lock()
	assert.Equal(t, true, expired)
	lock.Unlock()
	cache.Remove("key")

	lock.Lock()
	expired = false
	lock.Unlock()

	cache.SetWithTTL("key", "data", time.Millisecond*900)
	<-time.After(time.Millisecond * 500)
	cache.Touch("key")

	<-time.After(time.Millisecond * 500)
	lock.Lock()
	assert.Equal(t, false, expired)
	lock.Unlock()
}

// Issue #37: Cache metrics
func TestCache_TestMetrics(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Second)
	cache.Set("myKey", "myData")
	cache.SetWithTTL("myKey2", "myData", time.Second)

	cache.Get("myKey")
	cache.Get("myMiss")

	metrics := cache.GetMetrics()
	assert.Equal(t, int64(2), metrics.Inserted)
	assert.Equal(t, int64(1), metrics.Misses)
	assert.Equal(t, int64(2), metrics.Hits)
	assert.Equal(t, int64(1), metrics.Retrievals)
	cache.Purge()
	metrics = cache.GetMetrics()
	assert.Equal(t, int64(2), metrics.Evicted)

	cache.SetWithTTL("3", "3", time.Nanosecond)
	cache.SetWithTTL("4", "4", time.Nanosecond)
	cache.Count()
	time.Sleep(time.Millisecond * 10)

	metrics = cache.GetMetrics()
	assert.Equal(t, int64(4), metrics.Evicted)

}

// Issue #31: Test that a single fetch is executed with the loader function
func TestCache_TestSingleFetch(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	var calls int32

	loader := func(key string) (data interface{}, ttl time.Duration, err error) {
		time.Sleep(time.Millisecond * 100)
		atomic.AddInt32(&calls, 1)
		return "data", 0, nil

	}

	cache.SetLoaderFunction(loader)
	wg := sync.WaitGroup{}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			cache.Get("1")
			wg.Done()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls)
}

// Issue #30: Removal does not use expiration callback.
func TestCache_TestRemovalTriggersCallback(t *testing.T) {
	t.Parallel()
	cache := NewCache()
	defer cache.Close()

	var sync = make(chan struct{})
	expiration := func(key string, data interface{}) {

		sync <- struct{}{}
	}
	cache.SetExpirationCallback(expiration)

	cache.Set("1", "barf")
	cache.Remove("1")

	<-sync
}

// Issue #31: loader function
func TestCache_TestLoaderFunction(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		return nil, 0, ErrNotFound
	})

	_, err := cache.Get("1")
	assert.Equal(t, ErrNotFound, err)

	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		return "1", 0, nil
	})

	value, found := cache.Get("1")
	assert.Equal(t, nil, found)
	assert.Equal(t, "1", value)

	cache.Close()

	value, found = cache.Get("1")
	assert.Equal(t, ErrClosed, found)
	assert.Equal(t, nil, value)
}

// Issue #31: edge case where cache is closed when loader function has completed
func TestCache_TestLoaderFunctionDuringClose(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		cache.Close()
		return "1", 0, nil
	})

	value, found := cache.Get("1")
	assert.Equal(t, ErrClosed, found)
	assert.Equal(t, nil, value)

	cache.Close()

}

// Cache sometimes returns key not found under parallel access with a loader function
func TestCache_TestLoaderFunctionParallelKeyAccess(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		time.Sleep(time.Millisecond * 300)
		return "1", 1 * time.Nanosecond, nil
	})

	wg := sync.WaitGroup{}
	errCount := uint64(0)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, found := cache.Get("foo")
			if value != "1" || found != nil { // Use an atomic to avoid spamming logs
				atomic.AddUint64(&errCount, 1)
			}
		}()

	}

	wg.Wait()

	assert.Equalf(t, uint64(0), errCount, "expected 0 errs, got %d", errCount)

	cache.Close()
}

// Issue #28: call expirationCallback automatically on cache.Close()
func TestCache_ExpirationOnClose(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	success := make(chan struct{})
	defer close(success)

	cache.SetTTL(time.Hour * 100)
	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("%s\t%v", key, value)
		success <- struct{}{}
	})
	cache.Set("1", 1)
	cache.Set("2", 1)
	cache.Set("3", 1)

	found := 0
	cache.Close()
	wait := time.NewTimer(time.Millisecond * 100)
	for found != 3 {
		select {
		case <-success:
			found++
		case <-wait.C:
			t.Fail()
		}
	}

}

// # Issue 29: After Close() the behaviour of Get, Set, Remove is not defined.

func TestCache_ModifyAfterClose(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetTTL(time.Hour * 100)
	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("%s\t%v", key, value)
	})
	cache.Set("1", 1)
	cache.Set("2", 1)
	cache.Set("3", 1)

	_, findErr := cache.Get("1")
	assert.Equal(t, nil, findErr)
	assert.Equal(t, nil, cache.Set("broken", 1))
	assert.Equal(t, ErrNotFound, cache.Remove("broken2"))
	assert.Equal(t, nil, cache.Purge())
	assert.Equal(t, nil, cache.SetWithTTL("broken", 2, time.Minute))
	assert.Equal(t, nil, cache.SetTTL(time.Hour))

	cache.Close()

	_, getErr := cache.Get("broken3")
	assert.Equal(t, ErrClosed, getErr)
	assert.Equal(t, ErrClosed, cache.Set("broken", 1))
	assert.Equal(t, ErrClosed, cache.Remove("broken2"))
	assert.Equal(t, ErrClosed, cache.Purge())
	assert.Equal(t, ErrClosed, cache.SetWithTTL("broken", 2, time.Minute))
	assert.Equal(t, ErrClosed, cache.SetTTL(time.Hour))
	assert.Equal(t, 0, cache.Count())

}

// Issue #23: Goroutine leak on closing. When adding a close method i would like to see
// that it can be called in a repeated way without problems.
func TestCache_MultipleCloseCalls(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetTTL(time.Millisecond * 100)

	cache.SkipTTLExtensionOnHit(false)
	cache.Set("test", "!")
	startTime := time.Now()
	for now := time.Now(); now.Before(startTime.Add(time.Second * 3)); now = time.Now() {
		if _, err := cache.Get("test"); err != nil {
			t.Errorf("Item was not found, even though it should not expire.")
		}

	}

	cache.Close()
	assert.Equal(t, ErrClosed, cache.Close())
}

// test for Feature request in issue #12
//
func TestCache_SkipTtlExtensionOnHit(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Millisecond * 100)

	cache.SkipTTLExtensionOnHit(false)
	cache.Set("test", "!")
	startTime := time.Now()
	for now := time.Now(); now.Before(startTime.Add(time.Second * 3)); now = time.Now() {
		if _, err := cache.Get("test"); err != nil {
			t.Errorf("Item was not found, even though it should not expire.")
		}

	}

	cache.SkipTTLExtensionOnHit(true)
	cache.Set("expireTest", "!")
	// will loop if item does not expire
	for _, err := cache.Get("expireTest"); err == nil; _, err = cache.Get("expireTest") {
	}
}

func TestCache_ForRacesAcrossGoroutines(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Minute * 1)
	cache.SkipTTLExtensionOnHit(false)

	var wgSet sync.WaitGroup
	var wgGet sync.WaitGroup

	n := 500
	wgSet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgSet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				if i%2 == 0 {
					cache.Set(fmt.Sprintf("test%d", i/10), false)
				} else {
					cache.SetWithTTL(fmt.Sprintf("test%d", i/10), false, time.Second*59)
				}
				wgSet.Done()
			}(i)
		}
		wgSet.Done()
	}()
	wgGet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgGet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				cache.Get(fmt.Sprintf("test%d", i/10))
				wgGet.Done()
			}(i)
		}
		wgGet.Done()
	}()

	wgGet.Wait()
	wgSet.Wait()
}

func TestCache_SkipTtlExtensionOnHit_ForRacesAcrossGoroutines(t *testing.T) {
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Minute * 1)
	cache.SkipTTLExtensionOnHit(true)

	var wgSet sync.WaitGroup
	var wgGet sync.WaitGroup

	n := 500
	wgSet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgSet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				if i%2 == 0 {
					cache.Set(fmt.Sprintf("test%d", i/10), false)
				} else {
					cache.SetWithTTL(fmt.Sprintf("test%d", i/10), false, time.Second*59)
				}
				wgSet.Done()
			}(i)
		}
		wgSet.Done()
	}()
	wgGet.Add(1)
	go func() {
		for i := 0; i < n; i++ {
			wgGet.Add(1)

			go func(i int) {
				time.Sleep(time.Nanosecond * time.Duration(rand.Int63n(1000000)))
				cache.Get(fmt.Sprintf("test%d", i/10))
				wgGet.Done()
			}(i)
		}
		wgGet.Done()
	}()

	wgGet.Wait()
	wgSet.Wait()
}

// test github issue #14
// Testing expiration callback would continue with the next item in list, even when it exceeds list lengths
func TestCache_SetCheckExpirationCallback(t *testing.T) {
	t.Parallel()

	iterated := 0
	ch := make(chan struct{})

	cacheAD := NewCache()
	defer cacheAD.Close()

	cacheAD.SetTTL(time.Millisecond)
	cacheAD.SetCheckExpirationCallback(func(key string, value interface{}) bool {
		v := value.(*int)
		t.Logf("key=%v, value=%d\n", key, *v)
		iterated++
		if iterated == 1 {
			// this is the breaking test case for issue #14
			return false
		}
		ch <- struct{}{}
		return true
	})

	i := 2
	cacheAD.Set("a", &i)

	<-ch
}

// test github issue #9
// Due to scheduling the expected TTL of the top entry can become negative (already expired)
// This is an issue because negative TTL at the item level was interpreted as 'use global TTL'
// Which is not right when we become negative due to scheduling.
// This test could use improvement as it's not requiring a lot of time to trigger.
func TestCache_SetExpirationCallback(t *testing.T) {
	t.Parallel()

	type A struct {
	}

	// Setup the TTL cache
	cache := NewCache()
	defer cache.Close()

	ch := make(chan struct{}, 1024)
	cache.SetTTL(time.Second * 1)
	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("This key(%s) has expired\n", key)
		ch <- struct{}{}
	})
	for i := 0; i < 1024; i++ {
		cache.Set(fmt.Sprintf("item_%d", i), A{})
		time.Sleep(time.Millisecond * 10)
		t.Logf("Cache size: %d\n", cache.Count())
	}

	if cache.Count() > 100 {
		t.Fatal("Cache should empty entries >1 second old")
	}

	expired := 0
	for expired != 1024 {
		<-ch
		expired++
	}
}

// test github issue #4
func TestRemovalAndCountDoesNotPanic(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.Set("key", "value")
	cache.Remove("key")
	count := cache.Count()
	t.Logf("cache has %d keys\n", count)
}

// test github issue #3
func TestRemovalWithTtlDoesNotPanic(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("This key(%s) has expired\n", key)
	})

	cache.SetWithTTL("keyWithTTL", "value", time.Duration(2*time.Second))
	cache.Set("key", "value")
	cache.Remove("key")

	value, err := cache.Get("keyWithTTL")
	if err == nil {
		t.Logf("got %s for keyWithTTL\n", value)
	}
	count := cache.Count()
	t.Logf("cache has %d keys\n", count)

	<-time.After(3 * time.Second)

	value, err = cache.Get("keyWithTTL")
	if err != nil {
		t.Logf("got %s for keyWithTTL\n", value)
	} else {
		t.Logf("keyWithTTL has gone")
	}
	count = cache.Count()
	t.Logf("cache has %d keys\n", count)
}

func TestCacheIndividualExpirationBiggerThanGlobal(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	<-time.After(150 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, ErrNotFound, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpirationByGlobal(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.Set("key", "value")
	<-time.After(50 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, nil, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	data, exists = cache.Get("key")
	assert.Equal(t, exists, nil, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	<-time.After(100 * time.Millisecond)
	data, exists = cache.Get("key")
	assert.Equal(t, exists, ErrNotFound, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpiration(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_1", "value")
	cache.Set("key_2", "value")
	<-time.After(200 * time.Millisecond)
	assert.Equal(t, 0, cache.Count(), "Cache should be empty")

}

func TestCacheMixedExpirations(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetExpirationCallback(func(key string, value interface{}) {
		t.Logf("expired: %s", key)
	})
	cache.Set("key_1", "value")
	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(150 * time.Millisecond)
	assert.Equal(t, 1, cache.Count(), "Cache should have only 1 item")
}

func TestCacheIndividualExpiration(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key2", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key3", "value", time.Duration(100*time.Millisecond))
	<-time.After(50 * time.Millisecond)
	assert.Equal(t, cache.Count(), 3, "Should have 3 elements in cache")
	<-time.After(160 * time.Millisecond)
	assert.Equal(t, cache.Count(), 0, "Cache should be empty")

	cache.SetWithTTL("key4", "value", time.Duration(50*time.Millisecond))
	<-time.After(100 * time.Millisecond)
	<-time.After(100 * time.Millisecond)
	assert.Equal(t, 0, cache.Count(), "Cache should be empty")
}

func TestCacheGet(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	data, exists := cache.Get("hello")
	assert.Equal(t, exists, ErrNotFound, "Expected empty cache to return no data")
	assert.Nil(t, data, "Expected data to be empty")

	cache.Set("hello", "world")
	data, exists = cache.Get("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, nil, exists, "Expected data to exist")
	assert.Equal(t, "world", (data.(string)), "Expected data content to be 'world'")
}

func TestCacheGetKeys(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	keys := cache.GetKeys()
	assert.Empty(t, keys, "Expected keys to be empty")

	cache.Set("hello", "world")
	keys = cache.GetKeys()
	assert.NotEmpty(t, keys, "Expected keys to be not empty")
	assert.Equal(t, []string{"hello"}, keys, "Expected keys contains 'hello'")
}

func TestCacheGetItems(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	items := cache.GetItems()
	assert.Empty(t, items, "Expected items to be empty")

	cache.Set("hello", "world")
	items = cache.GetItems()
	assert.NotEmpty(t, items, "Expected items to be not empty")
	assert.Equal(t, map[string]interface{}{"hello": "world"}, items, "Expected items to {'hello': 'world'}")
}

func TestCacheGetWithTTL(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	data, ttl, exists := cache.GetWithTTL("hello")
	assert.Equal(t, exists, ErrNotFound, "Expected empty cache to return no data")
	assert.Nil(t, data, "Expected data to be empty")
	assert.Equal(t, int(ttl), 0, "Expected item TTL to be 0")

	cache.Set("hello", "world")
	data, ttl, exists = cache.GetWithTTL("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, nil, exists, "Expected data to exist")
	assert.Equal(t, "world", (data.(string)), "Expected data content to be 'world'")
	assert.Equal(t, int(ttl), 0, "Expected item TTL to be 0")

	orgttl := time.Duration(500 * time.Millisecond)
	cache.SetWithTTL("hello", "world", orgttl)
	time.Sleep(10 * time.Millisecond)
	data, ttl, exists = cache.GetWithTTL("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, nil, exists, "Expected data to exist")
	assert.Equal(t, "world", (data.(string)), "Expected data content to be 'world'")
	assert.Equal(t, ttl, orgttl, "Expected item TTL to be original TTL")

	cache.SkipTTLExtensionOnHit(true)
	cache.SetWithTTL("hello", "world", orgttl)
	time.Sleep(10 * time.Millisecond)
	data, ttl, exists = cache.GetWithTTL("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, nil, exists, "Expected data to exist")
	assert.Equal(t, "world", (data.(string)), "Expected data content to be 'world'")
	assert.Less(t, ttl, orgttl, "Expected item TTL to be less than the original TTL")
	assert.NotEqual(t, int(ttl), 0, "Expected item TTL to be not 0")
}

func TestCache_TestGetWithTTLAndLoaderFunction(t *testing.T) {
	t.Parallel()
	cache := NewCache()

	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		return nil, 0, ErrNotFound
	})

	_, ttl, err := cache.GetWithTTL("1")
	assert.Equal(t, ErrNotFound, err, "Expected error to be ErrNotFound")
	assert.Equal(t, int(ttl), 0, "Expected item TTL to be 0")

	orgttl := time.Duration(1 * time.Second)
	cache.SetLoaderFunction(func(key string) (data interface{}, ttl time.Duration, err error) {
		return "1", orgttl, nil
	})

	value, ttl, found := cache.GetWithTTL("1")
	assert.Equal(t, nil, found)
	assert.Equal(t, "1", value)
	assert.Equal(t, ttl, orgttl, "Expected item TTL to be the same as the original TTL")
	cache.Close()

	value, ttl, found = cache.GetWithTTL("1")
	assert.Equal(t, ErrClosed, found)
	assert.Equal(t, nil, value)
	assert.Equal(t, int(ttl), 0, "Expected returned ttl for an ErrClosed err to be 0")
}

func TestCacheExpirationCallbackFunction(t *testing.T) {
	t.Parallel()

	expiredCount := 0
	var lock sync.Mutex

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(500 * time.Millisecond))
	cache.SetExpirationCallback(func(key string, value interface{}) {
		lock.Lock()
		defer lock.Unlock()
		expiredCount = expiredCount + 1
	})
	cache.SetWithTTL("key", "value", time.Duration(1000*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(1100 * time.Millisecond)

	lock.Lock()
	defer lock.Unlock()
	assert.Equal(t, 2, expiredCount, "Expected 2 items to be expired")
}

// TestCacheCheckExpirationCallbackFunction should consider that the next entry in the queue
// needs to be considered for eviction even if the callback returns no eviction for the current item
func TestCacheCheckExpirationCallbackFunction(t *testing.T) {
	t.Parallel()

	expiredCount := 0
	var lock sync.Mutex

	cache := NewCache()
	defer cache.Close()

	cache.SkipTTLExtensionOnHit(true)
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetCheckExpirationCallback(func(key string, value interface{}) bool {
		if key == "key2" || key == "key4" {
			return true
		}
		return false
	})
	cache.SetExpirationCallback(func(key string, value interface{}) {
		lock.Lock()
		expiredCount = expiredCount + 1
		lock.Unlock()
	})
	cache.Set("key", "value")
	cache.Set("key3", "value")
	cache.Set("key2", "value")
	cache.Set("key4", "value")

	<-time.After(110 * time.Millisecond)
	lock.Lock()
	assert.Equal(t, 2, expiredCount, "Expected 2 items to be expired")
	lock.Unlock()
}

func TestCacheNewItemCallbackFunction(t *testing.T) {
	t.Parallel()

	newItemCount := 0
	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetNewItemCallback(func(key string, value interface{}) {
		newItemCount = newItemCount + 1
	})
	cache.Set("key", "value")
	cache.Set("key2", "value")
	cache.Set("key", "value")
	<-time.After(110 * time.Millisecond)
	assert.Equal(t, 2, newItemCount, "Expected only 2 new items")
}

func TestCacheRemove(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(70 * time.Millisecond)
	removeKey := cache.Remove("key")
	removeKey2 := cache.Remove("key_2")
	assert.Equal(t, nil, removeKey, "Expected 'key' to be removed from cache")
	assert.Equal(t, ErrNotFound, removeKey2, "Expected 'key_2' to already be expired from cache")
}

func TestCacheSetWithTTLExistItem(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(50*time.Millisecond))
	<-time.After(30 * time.Millisecond)
	cache.SetWithTTL("key", "value2", time.Duration(50*time.Millisecond))
	data, exists := cache.Get("key")
	assert.Equal(t, nil, exists, "Expected 'key' to exist")
	assert.Equal(t, "value2", data.(string), "Expected 'data' to have value 'value2'")
}

func TestCache_Purge(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Millisecond))

	for i := 0; i < 5; i++ {

		cache.SetWithTTL("key", "value", time.Duration(50*time.Millisecond))
		<-time.After(30 * time.Millisecond)
		cache.SetWithTTL("key", "value2", time.Duration(50*time.Millisecond))
		cache.Get("key")

		cache.Purge()
		assert.Equal(t, 0, cache.Count(), "Cache should be empty")
	}

}

func TestCache_Limit(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(100 * time.Second))
	cache.SetCacheSizeLimit(10)

	for i := 0; i < 100; i++ {
		cache.Set("key"+strconv.FormatInt(int64(i), 10), "value")
	}
	assert.Equal(t, 10, cache.Count(), "Cache should equal to limit")
	for i := 90; i < 100; i++ {
		key := "key" + strconv.FormatInt(int64(i), 10)
		val, _ := cache.Get(key)
		assert.Equal(t, "value", val, "Cache should be set [key90, key99]")
	}
}
