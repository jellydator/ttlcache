package ttlcache

import (
	"context"
	"errors"
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
// FetchFunc (or FetchManyFunc) panicked.
type FetchPanicError struct {
	Key   any  // the key being fetched; for a FetchManyFunc, the []K of keys
	Value any  // what the fetch panicked with
	many  bool // a FetchManyFunc panicked: Key holds its keys
}

func (e *FetchPanicError) Error() string {
	if e.many {
		return fmt.Sprintf("ttlcache: fetch of keys %v panicked: %v", e.Key, e.Value)
	}
	return fmt.Sprintf("ttlcache: fetch of key %v panicked: %v", e.Key, e.Value)
}

// ErrNotFound is what GetOrFetch returns when it joined a GetOrFetchMany
// fetch in flight that did not find its key: a FetchFunc always answers with
// a value or an error, a FetchManyFunc leaves out the keys it did not find.
var ErrNotFound = errors.New("ttlcache: the fetch in flight did not find the key")

// fetchCall is one key's in-flight fetch, shared by every caller that missed
// the key while it ran. A GetOrFetchMany fetch makes one per key, all closing
// the same done channel.
type fetchCall[V any] struct {
	done  chan struct{} // closed once val, found and err are set
	val   V
	found bool // val holds the key's value (always, for a FetchFunc that succeeded)
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
//   - A key in flight on a GetOrFetchMany is joined like any other; if that
//     fetch did not find it, GetOrFetch returns ErrNotFound.
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
		if call.err == nil && !call.found {
			return call.val, ErrNotFound // joined a GetOrFetchMany that did not find the key
		}
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

	call.val, call.found, call.err = val, err == nil, err
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
