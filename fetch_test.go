package ttlcache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedFetch is a FetchFunc that reports each start on started and returns
// value only once release is closed. Each call returns value plus its own
// call number, so tests can tell fetches apart.
type gatedFetch struct {
	value   string
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func newGatedFetch(value string) *gatedFetch {
	return &gatedFetch{value: value, started: make(chan struct{}, 16), release: make(chan struct{})}
}

func (g *gatedFetch) fetch(_ context.Context, _ string) (string, error) {
	n := g.calls.Add(1)
	g.started <- struct{}{}
	<-g.release
	return g.value + "#" + string(rune('0'+n)), nil
}

// cancelledCtx registers a caller with an in-flight fetch without waiting on
// it: the call joins (or starts) the fetch, then returns ctx.Err() at once.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// waitCached polls until key holds a value (the fetch goroutine caches it
// just before releasing its waiters, so a caller that left early has no
// other signal).
func waitCached[K comparable](t *testing.T, c *Cache[K, string], key K) string {
	t.Helper()
	var got string
	require.Eventually(t, func() bool {
		item := c.Get(key)
		if item == nil {
			return false
		}
		got = item.Value()
		return true
	}, time.Second, time.Millisecond)
	return got
}

func Test_Cache_GetOrFetch_MissFetchesAndCaches(t *testing.T) {
	c := New(WithTTL[string, string](time.Hour))
	var calls atomic.Int32
	fetch := func(_ context.Context, key string) (string, error) {
		calls.Add(1)
		return "v:" + key, nil
	}

	got, err := c.GetOrFetch(context.Background(), "k", time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, "v:k", got)

	item := c.Get("k")
	require.NotNil(t, item)
	assert.Equal(t, time.Minute, item.TTL()) // the ttl argument, not the cache default

	got, err = c.GetOrFetch(context.Background(), "k", time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, "v:k", got)
	assert.Equal(t, int32(1), calls.Load()) // the hit never fetched

	m := c.Metrics()
	assert.Equal(t, uint64(2), m.Hits) // the GetOrFetch hit + the Get above
	assert.Equal(t, uint64(1), m.Misses)
	assert.Empty(t, c.items.fetches)
}

func Test_Cache_GetOrFetch_DefaultTTL(t *testing.T) {
	c := New(WithTTL[string, string](time.Hour))
	_, err := c.GetOrFetch(context.Background(), "k", DefaultTTL, func(context.Context, string) (string, error) {
		return "v", nil
	})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, c.Get("k").TTL())
}

func Test_Cache_GetOrFetch_ConcurrentMissesShareOneFetch(t *testing.T) {
	c := New[string, string]()
	g := newGatedFetch("v")

	// deterministic: callers that gave up already still JOIN the in-flight fetch
	for range 5 {
		_, err := c.GetOrFetch(cancelledCtx(), "k", DefaultTTL, g.fetch)
		assert.ErrorIs(t, err, context.Canceled)
	}
	<-g.started

	// and live callers on other goroutines join it too
	const callers = 8
	results := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { results[i], errs[i] = c.GetOrFetch(context.Background(), "k", DefaultTTL, g.fetch) })
	}
	time.Sleep(50 * time.Millisecond) // let them reach the in-flight fetch
	close(g.release)
	wg.Wait()

	assert.Equal(t, int32(1), g.calls.Load())
	for i := range callers {
		require.NoError(t, errs[i])
		assert.Equal(t, "v#1", results[i])
	}
	assert.Equal(t, "v#1", c.Get("k").Value())
}

func Test_Cache_GetOrFetch_FetchIsDetachedFromTheStartersCancellation(t *testing.T) {
	type ctxKey struct{}
	c := New[string, string]()
	release := make(chan struct{})
	fetchErr := make(chan error, 1)
	fetchVal := make(chan any, 1)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "starter"))
	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrFetch(ctx, "k", DefaultTTL, func(fctx context.Context, _ string) (string, error) {
			<-release
			fetchErr <- fctx.Err()
			fetchVal <- fctx.Value(ctxKey{})
			return "v", nil
		})
		done <- err
	}()
	require.Eventually(t, func() bool { // the fetch is in flight
		c.items.mu.RLock()
		defer c.items.mu.RUnlock()
		return len(c.items.fetches) == 1
	}, time.Second, time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled) // the starter stops waiting at once...
	close(release)
	assert.NoError(t, <-fetchErr)          // ...while its fetch is not cancelled
	assert.Equal(t, "starter", <-fetchVal) // and still sees the starter's values
	assert.Equal(t, "v", waitCached(t, c, "k"))
}

func Test_Cache_GetOrFetch_ErrorIsSharedAndNotCached(t *testing.T) {
	c := New[string, string]()
	boom := errors.New("boom")
	var calls atomic.Int32
	failing := func(context.Context, string) (string, error) {
		calls.Add(1)
		return "", boom
	}

	_, err := c.GetOrFetch(context.Background(), "k", DefaultTTL, failing)
	require.ErrorIs(t, err, boom)
	assert.Nil(t, c.Get("k"))
	assert.Empty(t, c.items.fetches) // the key is not pinned by the failure

	_, err = c.GetOrFetch(context.Background(), "k", DefaultTTL, failing)
	require.ErrorIs(t, err, boom)
	assert.Equal(t, int32(2), calls.Load()) // a failure is retried by the next miss
}

func Test_Cache_GetOrFetch_PanicBecomesAnError(t *testing.T) {
	c := New[string, string]()
	_, err := c.GetOrFetch(context.Background(), "k", DefaultTTL, func(context.Context, string) (string, error) {
		panic("kaboom")
	})
	var perr *FetchPanicError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "k", perr.Key)
	assert.Equal(t, "kaboom", perr.Value)
	assert.Equal(t, "ttlcache: fetch of key k panicked: kaboom", err.Error())
	assert.Nil(t, c.Get("k"))
	assert.Empty(t, c.items.fetches)
}

// Each fencing write, applied while a fetch is in flight: the fetch is not
// cached, and the next miss starts a NEW fetch instead of joining the old.
func Test_Cache_GetOrFetch_ExplicitWritesFenceTheFetchInFlight(t *testing.T) {
	writes := map[string]func(c *Cache[string, string]){
		"Delete":       func(c *Cache[string, string]) { c.Delete("k") },
		"DeleteAll":    func(c *Cache[string, string]) { c.DeleteAll() },
		"GetAndDelete": func(c *Cache[string, string]) { c.GetAndDelete("k") },
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			c := New[string, string]()
			g := newGatedFetch("v")

			_, _ = c.GetOrFetch(cancelledCtx(), "k", DefaultTTL, g.fetch) // fetch #1 in flight
			<-g.started

			write(c)

			_, _ = c.GetOrFetch(cancelledCtx(), "k", DefaultTTL, g.fetch) // did not join #1
			<-g.started
			assert.Equal(t, int32(2), g.calls.Load())

			close(g.release)
			// only the post-write fetch is cached, whichever lands last
			assert.Eventually(t, func() bool {
				c.items.mu.RLock()
				defer c.items.mu.RUnlock()
				return len(c.items.fetches) == 0
			}, time.Second, time.Millisecond)
			got := waitCached(t, c, "k")
			assert.Equal(t, "v#2", got)
		})
	}
}

func Test_Cache_GetOrFetch_SetWinsOverTheFetchInFlight(t *testing.T) {
	setters := map[string]func(c *Cache[string, string]){
		"Set":          func(c *Cache[string, string]) { c.Set("k", "explicit", DefaultTTL) },
		"GetOrSet":     func(c *Cache[string, string]) { c.GetOrSet("k", "explicit") },
		"GetOrSetFunc": func(c *Cache[string, string]) { c.GetOrSetFunc("k", func() string { return "explicit" }) },
	}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			c := New[string, string]()
			g := newGatedFetch("v")
			waiter := make(chan string, 1)
			go func() {
				v, _ := c.GetOrFetch(context.Background(), "k", DefaultTTL, g.fetch)
				waiter <- v
			}()
			<-g.started

			set(c)
			close(g.release)

			assert.Equal(t, "v#1", <-waiter)                // the waiter still gets what it asked for...
			assert.Equal(t, "explicit", c.Get("k").Value()) // ...but the explicit value is not overwritten
		})
	}
}

func Test_Cache_GetOrFetch_OtherKeysAreNotFenced(t *testing.T) {
	c := New[string, string]()
	g := newGatedFetch("v")
	_, _ = c.GetOrFetch(cancelledCtx(), "k", DefaultTTL, g.fetch)
	<-g.started

	c.Delete("other")
	c.Set("other", "x", DefaultTTL)

	close(g.release)
	assert.Equal(t, "v#1", waitCached(t, c, "k"))
	assert.Equal(t, int32(1), g.calls.Load())
}
