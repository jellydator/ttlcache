package ttlcache

import (
	"context"
	"fmt"
	"time"
)

/* GetOrFetch: cache-aside loading where concurrent misses of a key share ONE
 * fetch, and an explicit write to the key fences a fetch already in flight.
 *
 * Unlike Loader (no context, no error, nothing fenced), it serves loads that
 * hit a database: the caller's context values reach the fetch, a failure is
 * an error rather than a nil item, and a value read before an invalidation
 * can never be cached after it. */

// FetchFunc loads the value of key on a miss (see GetOrFetch). ctx carries the
// values of the call that started the fetch but NOT its cancellation — bound
// the work yourself.
type FetchFunc[K comparable, V any] func(ctx context.Context, key K) (V, error)

// FetchPanicError is what every caller waiting on a fetch gets when its
// FetchFunc panicked.
type FetchPanicError struct {
	Key   any // the key being fetched
	Value any // what the FetchFunc panicked with
}

func (e *FetchPanicError) Error() string {
	return fmt.Sprintf("ttlcache: fetch of key %v panicked: %v", e.Key, e.Value)
}

// fetchCall is one in-flight fetch, shared by every caller that missed its
// key while it ran.
type fetchCall[V any] struct {
	done  chan struct{} // closed once val and err are set
	val   V
	err   error
	stale bool // fenced by an explicit write: do not cache val (guarded by items.mu)
}

// GetOrFetch returns the value of key: from the cache, else from fetch, run
// ONCE for every caller that misses the key while it is in flight. A
// successful result is cached with ttl (as in Set: DefaultTTL, NoTTL, …); an
// error is returned to every waiter and not cached.
//
//   - The fetch runs on its own goroutine, on a context detached from the
//     starting caller's cancellation. Each caller still stops waiting when its
//     own ctx ends, with ctx.Err(): one caller giving up never fails the
//     others, and a fetch started by a caller that already gave up still
//     lands in the cache for the next one. A fetch that never returns pins its
//     key, so bound it.
//   - An explicit write to the key while the fetch is in flight — Set,
//     Delete, DeleteAll, GetAndDelete, GetOrSet/GetOrSetFunc inserting —
//     FENCES it: its waiters still get its result, but the result is not
//     cached, and later callers start a new fetch instead of joining it. A
//     value read before an invalidation cannot outlive it. Expiry and
//     capacity evictions do not fence.
//   - A panicking fetch becomes a *FetchPanicError for every waiter.
//
// Hits and misses count as in Get, and a hit touches the item unless
// touch-on-hit is disabled. The cache's Loader is not consulted.
func (c *Cache[K, V]) GetOrFetch(ctx context.Context, key K, ttl time.Duration, fetch FetchFunc[K, V]) (V, error) {
	c.items.mu.Lock()
	if item := c.getWithOpts(key, false); item != nil {
		c.items.mu.Unlock()
		return item.Value(), nil
	}
	call, inFlight := c.items.fetches[key]
	if !inFlight {
		call = &fetchCall[V]{done: make(chan struct{})}
		c.items.fetches[key] = call
		go c.runFetch(context.WithoutCancel(ctx), key, ttl, fetch, call)
	}
	c.items.mu.Unlock()

	select {
	case <-call.done:
		return call.val, call.err
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

// runFetch runs one shared fetch, caches its result unless it failed or was
// fenced, and releases its waiters.
func (c *Cache[K, V]) runFetch(ctx context.Context, key K, ttl time.Duration, fetch FetchFunc[K, V], call *fetchCall[V]) {
	val, err := callFetch(ctx, key, fetch)

	c.items.mu.Lock()
	if c.items.fetches[key] == call {
		delete(c.items.fetches, key)
	}
	if err == nil && !call.stale {
		c.set(key, val, ttl)
	}
	c.items.mu.Unlock()

	call.val, call.err = val, err
	close(call.done)
}

// callFetch runs fetch, turning a panic into a *FetchPanicError: the fetch
// runs on a goroutine of its own, beyond the reach of any caller's recover.
func callFetch[K comparable, V any](ctx context.Context, key K, fetch FetchFunc[K, V]) (val V, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero V
			val, err = zero, &FetchPanicError{Key: key, Value: r}
		}
	}()
	return fetch(ctx, key)
}

// fenceFetch marks key's in-flight fetch stale and detaches it from the key,
// so the next miss starts a new fetch.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) fenceFetch(key K) {
	if call, ok := c.items.fetches[key]; ok {
		call.stale = true
		delete(c.items.fetches, key)
	}
}

// fenceAllFetches fences every in-flight fetch.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) fenceAllFetches() {
	for key, call := range c.items.fetches {
		call.stale = true
		delete(c.items.fetches, key)
	}
}
