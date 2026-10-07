package ttlcache

import (
	"context"
	"slices"
	"time"
)

/* GetOrFetchMany: GetOrFetch for a batch of keys. Coalescing and fencing
 * stay PER KEY, so the batch keeps its one load without losing either:
 * a key already being fetched (by GetOrFetch or another GetOrFetchMany) is
 * waited on, and every other missed key goes into ONE new fetch.
 *
 * Example, all while A's fetch is in flight:
 *   A asks 1, 2, 3 — nothing in flight: one fetch of [1 2 3];
 *   B asks 2, 3, 4 — waits on A for 2 and 3, one fetch of [4];
 *   C asks 3, 2, 1 — waits on A for all three, fetches nothing.
 * Two loads in all, each key loaded once. */

// FetchManyFunc loads the values of keys on a miss (see GetOrFetchMany). keys
// are distinct, in the order the caller first asked them, and the slice is the
// fetch's own. It returns the values it found, by key: a key it leaves out is
// not found — left out of the result and not cached — and a value of a key it
// was not asked is ignored. ctx carries the values of the call that started
// the fetch but NOT its cancellation — bound the work yourself.
type FetchManyFunc[K comparable, V any] func(ctx context.Context, keys []K) (map[K]V, error)

// pendingKey is a key GetOrFetchMany waits on, with the fetch loading it.
type pendingKey[K comparable, V any] struct {
	key  K
	call *fetchCall[V]
}

// GetOrFetchMany returns the values of keys: from the cache, else from fetch.
// The keys the cache misses and no fetch is loading go into ONE call of fetch;
// the keys already being fetched are waited on. Every key is still its own
// in-flight fetch, so GetOrFetch callers join these and the other way round.
// Found values are cached with ttl (as in Set), in the order asked; keys not
// found are left out of the result and not cached. Duplicated keys count once.
// The result is the caller's own map, never nil.
//
//   - The fetch runs on its own goroutine, on a context detached from the
//     starting caller's cancellation, as in GetOrFetch: each caller stops
//     waiting on its own ctx, with ctx.Err(), and a fetch whose starter gave
//     up still lands in the cache.
//   - An explicit write to a key while it is being fetched fences THAT key, as
//     in GetOrFetch: its waiters still get the fetched value, but it is not
//     cached. The other keys of the same fetch are cached.
//   - An error, or a panic (a *FetchPanicError whose Key is the []K fetched),
//     goes to every caller waiting on any key of that fetch, and nothing of it
//     is cached. A caller gets no values and the error of the first key, in
//     the order asked, whose fetch failed.
//
// Hits and misses count per key as in Get, and a hit touches the item unless
// touch-on-hit is disabled. The cache's Loader is not consulted.
func (c *Cache[K, V]) GetOrFetchMany(ctx context.Context, keys []K, ttl time.Duration, fetch FetchManyFunc[K, V]) (map[K]V, error) {
	out := make(map[K]V, len(keys))
	seen := make(map[K]struct{}, len(keys))
	done := make(chan struct{}) // closed by the fetch this call starts, if any
	var waits []pendingKey[K, V]
	var missed []K
	var calls []*fetchCall[V]

	c.items.mu.Lock()
	for _, key := range keys {
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if item := c.getWithOpts(key, false); item != nil {
			out[key] = item.Value()
			continue
		}
		call, inFlight := c.items.fetches[key]
		if !inFlight {
			call = &fetchCall[V]{done: done}
			c.items.fetches[key] = call
			missed = append(missed, key)
			calls = append(calls, call)
		}
		waits = append(waits, pendingKey[K, V]{key, call})
	}
	if len(missed) > 0 {
		go c.runFetchMany(context.WithoutCancel(ctx), missed, ttl, fetch, calls, done)
	}
	c.items.mu.Unlock()

	for _, w := range waits {
		select {
		case <-w.call.done:
			if w.call.err != nil {
				return nil, w.call.err
			}
			if w.call.found {
				out[w.key] = w.call.val
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return out, nil
}

// runFetchMany runs one shared batch fetch, caches what it found (save the
// fenced keys), and releases every key's waiters. calls[i] is keys[i]'s.
func (c *Cache[K, V]) runFetchMany(ctx context.Context, keys []K, ttl time.Duration, fetch FetchManyFunc[K, V], calls []*fetchCall[V], done chan struct{}) {
	vals, err := callFetchMany(ctx, keys, fetch)

	c.items.mu.Lock()
	for i, key := range keys {
		call := calls[i]
		if c.items.fetches[key] == call {
			delete(c.items.fetches, key)
		}
		call.err = err
		if err != nil {
			continue
		}
		if val, ok := vals[key]; ok {
			call.val, call.found = val, true
			if !call.stale {
				c.set(key, val, ttl)
			}
		}
	}
	c.items.mu.Unlock()

	close(done)
}

// callFetchMany runs fetch on its own copy of keys, turning a panic into a
// *FetchPanicError: the fetch runs on a goroutine of its own.
func callFetchMany[K comparable, V any](ctx context.Context, keys []K, fetch FetchManyFunc[K, V]) (vals map[K]V, err error) {
	defer func() {
		if r := recover(); r != nil {
			vals, err = nil, &FetchPanicError{Key: keys, Value: r, many: true}
		}
	}()
	return fetch(ctx, slices.Clone(keys))
}
