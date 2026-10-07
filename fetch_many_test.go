package ttlcache

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fetched is the value batchFetch answers for key on its call-th call.
func fetched(key, call int) string { return fmt.Sprintf("v%d#%d", key, call) }

// findAll answers every key asked.
func findAll(call int, keys []int) (map[int]string, error) {
	vals := make(map[int]string, len(keys))
	for _, k := range keys {
		vals[k] = fetched(k, call)
	}
	return vals, nil
}

// batchFetch is a FetchManyFunc that records the keys of each call, reports
// each start on started (the call number, from 1) and answers only once that
// call is released — with answer, findAll unless the test sets another before
// the first fetch.
type batchFetch struct {
	mu      sync.Mutex
	batches [][]int
	gates   map[int]chan struct{}
	open    bool // every call is released, present and future
	started chan int
	answer  func(call int, keys []int) (map[int]string, error)
}

func newBatchFetch(t *testing.T) *batchFetch {
	b := &batchFetch{gates: map[int]chan struct{}{}, started: make(chan int, 64), answer: findAll}
	t.Cleanup(b.releaseAll) // no fetch goroutine outlives its test
	return b
}

// newOpenFetch is a batchFetch whose calls answer at once.
func newOpenFetch(t *testing.T) *batchFetch {
	b := newBatchFetch(t)
	b.releaseAll()
	return b
}

// gate returns call's gate; b.mu must be held.
func (b *batchFetch) gate(call int) chan struct{} {
	g, ok := b.gates[call]
	if !ok {
		g = make(chan struct{})
		if b.open {
			close(g)
		}
		b.gates[call] = g
	}
	return g
}

func (b *batchFetch) fetch(_ context.Context, keys []int) (map[int]string, error) {
	b.mu.Lock()
	b.batches = append(b.batches, slices.Clone(keys))
	call := len(b.batches)
	gate := b.gate(call)
	b.mu.Unlock()

	b.started <- call
	<-gate
	return b.answer(call, keys)
}

func (b *batchFetch) release(call int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if g := b.gate(call); !isClosed(g) {
		close(g)
	}
}

func (b *batchFetch) releaseAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open = true
	for _, g := range b.gates {
		if !isClosed(g) {
			close(g)
		}
	}
}

// calls returns the keys each fetch was asked, in call order.
func (b *batchFetch) calls() [][]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.batches)
}

// awaitStart waits for the next fetch to start and returns its call number.
func (b *batchFetch) awaitStart(t *testing.T) int {
	t.Helper()
	select {
	case call := <-b.started:
		return call
	case <-time.After(time.Second):
		t.Fatal("no fetch started")
		return 0
	}
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

type manyResult struct {
	vals map[int]string
	err  error
}

type oneResult struct {
	val string
	err error
}

// goFetchMany runs GetOrFetchMany on its own goroutine.
func goFetchMany(ctx context.Context, c *Cache[int, string], keys []int, ttl time.Duration, fetch FetchManyFunc[int, string]) <-chan manyResult {
	ch := make(chan manyResult, 1)
	go func() {
		vals, err := c.GetOrFetchMany(ctx, keys, ttl, fetch)
		ch <- manyResult{vals, err}
	}()
	return ch
}

// goFetch runs GetOrFetch on its own goroutine.
func goFetch(ctx context.Context, c *Cache[int, string], key int, fetch FetchFunc[int, string]) <-chan oneResult {
	ch := make(chan oneResult, 1)
	go func() {
		val, err := c.GetOrFetch(ctx, key, DefaultTTL, fetch)
		ch <- oneResult{val, err}
	}()
	return ch
}

func await[R any](t *testing.T, ch <-chan R) R {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(time.Second):
		t.Fatal("the call did not return")
		var zero R
		return zero
	}
}

// stillWaiting asserts the call has not returned yet.
func stillWaiting[R any](t *testing.T, ch <-chan R) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("returned early: %+v", r)
	case <-time.After(20 * time.Millisecond):
	}
}

// waitMisses waits until the cache counted n misses. A caller counts its
// misses while it holds the lock under which it registers its waits and its
// fetch, so once they show, the next locked operation runs after that.
func waitMisses[K comparable, V any](t *testing.T, c *Cache[K, V], n uint64) {
	t.Helper()
	require.Eventually(t, func() bool { return c.Metrics().Misses >= n }, time.Second, time.Millisecond)
	require.Equal(t, n, c.Metrics().Misses)
}

func waitNoFetches[K comparable, V any](t *testing.T, c *Cache[K, V]) {
	t.Helper()
	require.Eventually(t, func() bool { return inFlight(c) == 0 }, time.Second, time.Millisecond)
}

func inFlight[K comparable, V any](c *Cache[K, V]) int {
	c.items.mu.RLock()
	defer c.items.mu.RUnlock()
	return len(c.items.fetches)
}

// cached reads what the cache holds for key without counting a hit or a miss.
func cached(c *Cache[int, string], key int) (string, bool) {
	item, ok := c.Items()[key]
	if !ok {
		return "", false
	}
	return item.Value(), true
}

func mustNotFetch(t *testing.T) FetchFunc[int, string] {
	return func(context.Context, int) (string, error) {
		t.Error("GetOrFetch ran its own fetch instead of joining the one in flight")
		return "", nil
	}
}

/* ---- the plain path: hits, misses, what is fetched and cached ---- */

func Test_Cache_GetOrFetchMany_MissesShareOneFetchAndAreCached(t *testing.T) {
	c := New(WithTTL[int, string](time.Hour))
	c.Set(1, "cached", DefaultTTL)
	b := newOpenFetch(t)

	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2, 3}, time.Minute, b.fetch)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: "cached", 2: fetched(2, 1), 3: fetched(3, 1)}, got)
	assert.Equal(t, [][]int{{2, 3}}, b.calls()) // ONE fetch, of the misses only

	m := c.Metrics()
	assert.Equal(t, uint64(1), m.Hits)
	assert.Equal(t, uint64(2), m.Misses)
	assert.Equal(t, uint64(3), m.Insertions) // the Set + the two fetched
	items := c.Items()
	assert.Equal(t, time.Minute, items[2].TTL()) // the ttl argument, not the cache default
	assert.Equal(t, time.Minute, items[3].TTL())
	assert.Equal(t, time.Hour, items[1].TTL()) // a hit is not rewritten
	assert.Equal(t, "cached", items[1].Value())

	again, err := c.GetOrFetchMany(context.Background(), []int{3, 2, 1}, time.Minute, b.fetch)
	require.NoError(t, err)
	assert.Equal(t, got, again)
	assert.Len(t, b.calls(), 1) // all hits now
	assert.Equal(t, uint64(4), c.Metrics().Hits)
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_NoKeysFetchNothing(t *testing.T) {
	for name, keys := range map[string][]int{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			c := New[int, string]()
			b := newOpenFetch(t)
			for _, ctx := range []context.Context{context.Background(), cancelledCtx()} {
				got, err := c.GetOrFetchMany(ctx, keys, DefaultTTL, b.fetch)
				require.NoError(t, err) // nothing to wait on: even a cancelled caller succeeds
				require.NotNil(t, got)
				assert.Empty(t, got)
			}
			assert.Empty(t, b.calls())
			assert.Equal(t, Metrics{}, c.Metrics())
			assert.Zero(t, inFlight(c))
		})
	}
}

func Test_Cache_GetOrFetchMany_AllHitsFetchNothingEvenWhenCancelled(t *testing.T) {
	c := New[int, string]()
	c.Set(1, "a", DefaultTTL)
	c.Set(2, "b", DefaultTTL)
	b := newOpenFetch(t)

	got, err := c.GetOrFetchMany(cancelledCtx(), []int{1, 2}, DefaultTTL, b.fetch)
	require.NoError(t, err) // as GetOrFetch: a hit is served whatever the context
	assert.Equal(t, map[int]string{1: "a", 2: "b"}, got)
	assert.Empty(t, b.calls())
	assert.Equal(t, uint64(2), c.Metrics().Hits)
	assert.Zero(t, c.Metrics().Misses)
}

func Test_Cache_GetOrFetchMany_DuplicateKeysCountOnce(t *testing.T) {
	c := New[int, string]()
	c.Set(1, "cached", DefaultTTL)
	b := newBatchFetch(t)

	// 5 is in flight on another fetch
	_, _ = c.GetOrFetchMany(cancelledCtx(), []int{5}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))

	res := goFetchMany(context.Background(), c, []int{3, 1, 5, 2, 1, 3, 5, 2, 3}, DefaultTTL, b.fetch)
	require.Equal(t, 2, b.awaitStart(t))
	waitMisses(t, c, 1+3) // 3, 5, 2 — each once
	assert.Equal(t, uint64(1), c.Metrics().Hits)
	// the new misses go out once each, in the order first asked
	assert.Equal(t, [][]int{{5}, {3, 2}}, b.calls())

	b.releaseAll()
	r := await(t, res)
	require.NoError(t, r.err)
	assert.Equal(t, map[int]string{1: "cached", 2: fetched(2, 2), 3: fetched(3, 2), 5: fetched(5, 1)}, r.vals)
}

func Test_Cache_GetOrFetchMany_NotFoundKeysAreLeftOutAndNotCached(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	b.answer = func(call int, keys []int) (map[int]string, error) { // finds the even keys only
		vals := map[int]string{}
		for _, k := range keys {
			if k%2 == 0 {
				vals[k] = fetched(k, call)
			}
		}
		return vals, nil
	}

	starter := goFetchMany(context.Background(), c, []int{1, 2, 3, 4}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	joiner := goFetchMany(context.Background(), c, []int{3, 4}, DefaultTTL, b.fetch)
	waitMisses(t, c, 4+2)
	b.release(1)

	r := await(t, starter)
	require.NoError(t, r.err) // not found is no error
	assert.Equal(t, map[int]string{2: fetched(2, 1), 4: fetched(4, 1)}, r.vals)
	j := await(t, joiner)
	require.NoError(t, j.err)
	assert.Equal(t, map[int]string{4: fetched(4, 1)}, j.vals) // a joiner sees the same absence
	assert.False(t, c.Has(1))
	assert.False(t, c.Has(3))
	assert.Equal(t, 2, c.Len())
	assert.Zero(t, inFlight(c)) // the not-found keys are not left pinned

	// not cached: the next miss asks again
	b.releaseAll()
	r2, err := c.GetOrFetchMany(context.Background(), []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{2: fetched(2, 1)}, r2)
	assert.Equal(t, [][]int{{1, 2, 3, 4}, {1, 3}}, b.calls())
}

func Test_Cache_GetOrFetchMany_ZeroValueIsFound(t *testing.T) {
	c := New[int, string]()
	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, DefaultTTL, func(context.Context, []int) (map[int]string, error) {
		return map[int]string{1: ""}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: ""}, got) // present with "", unlike 2
	v, ok := cached(c, 1)
	assert.True(t, ok)
	assert.Empty(t, v)
	assert.False(t, c.Has(2))
}

func Test_Cache_GetOrFetchMany_NilAnswerFindsNothing(t *testing.T) {
	c := New[int, string]()
	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, DefaultTTL, func(context.Context, []int) (map[int]string, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got)
	assert.Zero(t, c.Len())
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_ValuesOfKeysNotAskedAreIgnored(t *testing.T) {
	c := New[int, string]()
	got, err := c.GetOrFetchMany(context.Background(), []int{1}, DefaultTTL, func(context.Context, []int) (map[int]string, error) {
		return map[int]string{1: "a", 99: "stray"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: "a"}, got)
	assert.False(t, c.Has(99)) // a key no fetch was registered for is never cached
	assert.Equal(t, 1, c.Len())
}

func Test_Cache_GetOrFetchMany_TTL(t *testing.T) {
	tests := map[string]struct {
		ttl  time.Duration
		want time.Duration
	}{
		"explicit":   {time.Minute, time.Minute},
		"DefaultTTL": {DefaultTTL, time.Hour},
		"NoTTL":      {NoTTL, NoTTL},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := New(WithTTL[int, string](time.Hour))
			_, err := c.GetOrFetchMany(context.Background(), []int{1}, tc.ttl, newOpenFetch(t).fetch)
			require.NoError(t, err)
			item := c.Items()[1]
			require.NotNil(t, item)
			assert.Equal(t, tc.want, item.TTL())
			if tc.want == NoTTL {
				assert.True(t, item.ExpiresAt().IsZero())
			}
		})
	}
}

func Test_Cache_GetOrFetchMany_JoinedKeysKeepTheStartersTTL(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	_, _ = c.GetOrFetchMany(cancelledCtx(), []int{1}, time.Minute, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	res := goFetchMany(context.Background(), c, []int{1, 2}, time.Hour, b.fetch)
	require.Equal(t, 2, b.awaitStart(t))

	b.releaseAll()
	require.NoError(t, await(t, res).err)
	items := c.Items()
	assert.Equal(t, time.Minute, items[1].TTL()) // fetched by the starter, with its ttl
	assert.Equal(t, time.Hour, items[2].TTL())
}

func Test_Cache_GetOrFetchMany_ExpiredItemsAreFetchedAgain(t *testing.T) {
	c := New[int, string]()
	c.Set(1, "old", time.Millisecond)
	c.Set(2, "fresh", time.Hour)
	time.Sleep(5 * time.Millisecond)
	b := newOpenFetch(t)

	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, time.Hour, b.fetch)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: fetched(1, 1), 2: "fresh"}, got)
	assert.Equal(t, [][]int{{1}}, b.calls())
	m := c.Metrics()
	assert.Equal(t, uint64(1), m.Misses)  // an expired item is a miss
	assert.Equal(t, uint64(1), m.Updates) // rewritten in place
	assert.Equal(t, time.Hour, c.Items()[1].TTL())
}

func Test_Cache_GetOrFetchMany_HitsTouchUnlessDisabled(t *testing.T) {
	tests := map[string]struct {
		opts    []Option[int, string]
		touched bool
	}{
		"touch on hit":          {nil, true},
		"touch on hit disabled": {[]Option[int, string]{WithDisableTouchOnHit[int, string]()}, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := New(tc.opts...)
			c.Set(1, "a", time.Hour)
			before := c.Items()[1].ExpiresAt()
			time.Sleep(2 * time.Millisecond)

			_, err := c.GetOrFetchMany(context.Background(), []int{1}, DefaultTTL, newOpenFetch(t).fetch)
			require.NoError(t, err)
			after := c.Items()[1].ExpiresAt()
			if tc.touched {
				assert.True(t, after.After(before))
			} else {
				assert.Equal(t, before, after)
			}
		})
	}
}

func Test_Cache_GetOrFetchMany_TheLoaderIsNotConsulted(t *testing.T) {
	var loads atomic.Int32
	c := New(WithLoader[int, string](LoaderFunc[int, string](func(*Cache[int, string], int) *Item[int, string] {
		loads.Add(1)
		return nil
	})))
	got, err := c.GetOrFetchMany(context.Background(), []int{1}, DefaultTTL, newOpenFetch(t).fetch)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: fetched(1, 1)}, got)
	assert.Zero(t, loads.Load())
}

func Test_Cache_GetOrFetchMany_CachesInTheOrderAsked(t *testing.T) {
	c := New(WithCapacity[int, string](2))
	got, err := c.GetOrFetchMany(context.Background(), []int{3, 1, 2}, DefaultTTL, newOpenFetch(t).fetch)
	require.NoError(t, err)
	assert.Len(t, got, 3) // the caller gets every value, cached or not
	keys := c.Keys()
	slices.Sort(keys)
	assert.Equal(t, []int{1, 2}, keys) // 3 went in first, so it was the one evicted
	assert.Equal(t, uint64(1), c.Metrics().Evictions)
}

func Test_Cache_GetOrFetchMany_FetchedItemsAreInsertions(t *testing.T) {
	c := New[int, string]()
	inserted := make(chan int, 4)
	unsubscribe := c.OnInsertion(func(_ context.Context, item *Item[int, string]) { inserted <- item.Key() })

	_, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, DefaultTTL, newOpenFetch(t).fetch)
	require.NoError(t, err)
	unsubscribe() // waits for the event handlers
	close(inserted)
	var keys []int
	for k := range inserted {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	assert.Equal(t, []int{1, 2}, keys)
}

func Test_Cache_GetOrFetchMany_TheResultIsTheCallersOwn(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	asked := []int{2, 1}
	first := goFetchMany(context.Background(), c, asked, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	second := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
	waitMisses(t, c, 4)
	b.release(1)

	r1, r2 := await(t, first), await(t, second)
	require.NoError(t, r1.err)
	require.NoError(t, r2.err)
	r1.vals[1] = "mutated"
	delete(r1.vals, 2)
	assert.Equal(t, map[int]string{1: fetched(1, 1), 2: fetched(2, 1)}, r2.vals)
	v, _ := cached(c, 1)
	assert.Equal(t, fetched(1, 1), v)
	assert.Equal(t, []int{2, 1}, asked) // the caller's keys are left as they were
}

func Test_Cache_GetOrFetchMany_TheFetchOwnsItsKeys(t *testing.T) {
	c := New[int, string]()
	got, err := c.GetOrFetchMany(context.Background(), []int{3, 1, 2}, DefaultTTL, func(_ context.Context, keys []int) (map[int]string, error) {
		vals, _ := findAll(1, keys)
		for i := range keys { // a fetch may scribble on its keys
			keys[i] = -1
		}
		return vals, nil
	})
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: fetched(1, 1), 2: fetched(2, 1), 3: fetched(3, 1)}, got)
	assert.Equal(t, 3, c.Len())
	assert.Zero(t, inFlight(c)) // the keys still unregistered: none left pinned
	assert.False(t, c.Has(-1))
}

/* ---- per-key coalescing ---- */

// The example of the design: A asks 1 2 3, B asks 2 3 4, C asks 3 2 1, all
// while A's fetch is in flight — two loads in all, each key loaded once.
func Test_Cache_GetOrFetchMany_CoalescesPerKey(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	ctx := context.Background()

	a := goFetchMany(ctx, c, []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	bb := goFetchMany(ctx, c, []int{2, 3, 4}, DefaultTTL, b.fetch)
	require.Equal(t, 2, b.awaitStart(t))
	cc := goFetchMany(ctx, c, []int{3, 2, 1}, DefaultTTL, b.fetch)
	waitMisses(t, c, 3+3+3) // keys in flight are misses too
	assert.Equal(t, [][]int{{1, 2, 3}, {4}}, b.calls())
	assert.Equal(t, 4, inFlight(c))

	b.release(2) // B's own key lands, but B still waits on A's 2 and 3
	assert.Equal(t, fetched(4, 2), waitCached(t, c, 4))
	stillWaiting(t, bb)
	stillWaiting(t, cc)
	stillWaiting(t, a)

	b.release(1)
	want := map[int]string{1: fetched(1, 1), 2: fetched(2, 1), 3: fetched(3, 1)}
	ra, rb, rc := await(t, a), await(t, bb), await(t, cc)
	require.NoError(t, ra.err)
	require.NoError(t, rb.err)
	require.NoError(t, rc.err)
	assert.Equal(t, want, ra.vals)
	assert.Equal(t, map[int]string{2: fetched(2, 1), 3: fetched(3, 1), 4: fetched(4, 2)}, rb.vals)
	assert.Equal(t, want, rc.vals)
	assert.Len(t, b.calls(), 2)
	assert.Equal(t, 4, c.Len())
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_ConcurrentOverlappingBatchesLoadEachKeyOnce(t *testing.T) {
	const keys, callers, rounds = 64, 32, 20
	rng := rand.New(rand.NewPCG(1, 2))
	for round := range rounds {
		c := New[int, string]()
		var loads [keys]atomic.Int32
		fetch := func(_ context.Context, ks []int) (map[int]string, error) {
			time.Sleep(time.Millisecond) // keep it in flight while others ask
			vals := make(map[int]string, len(ks))
			for _, k := range ks {
				loads[k].Add(1)
				vals[k] = fmt.Sprint("v", k)
			}
			return vals, nil
		}

		asks := make([][]int, callers)
		for i := range asks {
			for range 1 + rng.IntN(16) {
				asks[i] = append(asks[i], rng.IntN(keys))
			}
		}
		results := make([]map[int]string, callers)
		errs := make([]error, callers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range callers {
			wg.Go(func() {
				<-start
				results[i], errs[i] = c.GetOrFetchMany(context.Background(), asks[i], DefaultTTL, fetch)
			})
		}
		close(start)
		wg.Wait()

		asked := map[int]bool{}
		for i := range callers {
			require.NoError(t, errs[i])
			want := map[int]string{}
			for _, k := range asks[i] {
				asked[k] = true
				want[k] = fmt.Sprint("v", k)
			}
			assert.Equal(t, want, results[i], "round %d caller %d", round, i)
		}
		for k := range keys {
			want := int32(0)
			if asked[k] {
				want = 1
			}
			assert.Equal(t, want, loads[k].Load(), "round %d: loads of key %d", round, k)
		}
		assert.Zero(t, inFlight(c))
	}
}

/* ---- cancellation ---- */

func Test_Cache_GetOrFetchMany_FetchIsDetachedFromTheStartersCancellation(t *testing.T) {
	type ctxKey struct{}
	c := New[int, string]()
	release := make(chan struct{})
	fetchErr := make(chan error, 1)
	fetchVal := make(chan any, 1)
	fetch := func(fctx context.Context, keys []int) (map[int]string, error) {
		<-release
		fetchErr <- fctx.Err()
		fetchVal <- fctx.Value(ctxKey{})
		return findAll(1, keys)
	}

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "starter"))
	starter := goFetchMany(ctx, c, []int{1, 2}, DefaultTTL, fetch)
	waitMisses(t, c, 2)
	joiner := goFetchMany(context.Background(), c, []int{2}, DefaultTTL, fetch)
	waitMisses(t, c, 3)

	cancel()
	r := await(t, starter) // the starter stops waiting at once...
	require.ErrorIs(t, r.err, context.Canceled)
	assert.Nil(t, r.vals)
	close(release)
	require.NoError(t, <-fetchErr)         // ...while its fetch is not cancelled
	assert.Equal(t, "starter", <-fetchVal) // and still sees the starter's values
	j := await(t, joiner)                  // the joiner is served
	require.NoError(t, j.err)
	assert.Equal(t, map[int]string{2: fetched(2, 1)}, j.vals)
	assert.Equal(t, fetched(1, 1), waitCached(t, c, 1)) // and all of it is cached
}

func Test_Cache_GetOrFetchMany_EachCallerStopsOnItsOwnContext(t *testing.T) {
	stoppers := map[string]struct {
		ctx       func() (context.Context, context.CancelFunc)
		cancelNow bool // else the deadline stops it
		want      error
	}{
		"cancelled": {func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, true, context.Canceled},
		"deadline": {func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 10*time.Millisecond)
		}, false, context.DeadlineExceeded},
	}
	for name, tc := range stoppers {
		t.Run(name, func(t *testing.T) {
			c := New[int, string]()
			b := newBatchFetch(t)
			other := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
			require.Equal(t, 1, b.awaitStart(t))

			ctx, cancel := tc.ctx()
			defer cancel()
			mine := goFetchMany(ctx, c, []int{2, 3}, DefaultTTL, b.fetch) // waits on 2, fetches 3
			require.Equal(t, 2, b.awaitStart(t))
			if tc.cancelNow {
				cancel()
			}

			r := await(t, mine) // both fetches still gated
			require.ErrorIs(t, r.err, tc.want)
			assert.Nil(t, r.vals)

			b.releaseAll()
			o := await(t, other) // another caller is not affected
			require.NoError(t, o.err)
			assert.Equal(t, map[int]string{1: fetched(1, 1), 2: fetched(2, 1)}, o.vals)
			assert.Equal(t, fetched(3, 2), waitCached(t, c, 3)) // what the leaver started still lands
			waitNoFetches(t, c)
		})
	}
}

func Test_Cache_GetOrFetchMany_AnAlreadyCancelledCallerStillStartsTheFetch(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	c.Set(1, "cached", DefaultTTL)

	got, err := c.GetOrFetchMany(cancelledCtx(), []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got) // no partial result, not even the hit
	require.Equal(t, 1, b.awaitStart(t))
	assert.Equal(t, [][]int{{2, 3}}, b.calls())

	b.release(1)
	assert.Equal(t, fetched(2, 1), waitCached(t, c, 2))
	assert.Equal(t, fetched(3, 1), waitCached(t, c, 3))
	waitNoFetches(t, c)
}

/* ---- errors and panics ---- */

func Test_Cache_GetOrFetchMany_ErrorIsSharedAndNotCached(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	boom := errors.New("boom")
	b.answer = func(call int, keys []int) (map[int]string, error) {
		if call == 1 {
			return map[int]string{1: "partial"}, boom // values beside an error count for nothing
		}
		return findAll(call, keys)
	}

	starter := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	joiner := goFetchMany(context.Background(), c, []int{2}, DefaultTTL, b.fetch)
	single := goFetch(context.Background(), c, 1, mustNotFetch(t))
	waitMisses(t, c, 2+1+1)
	b.release(1)

	for _, r := range []manyResult{await(t, starter), await(t, joiner)} {
		require.ErrorIs(t, r.err, boom)
		assert.Nil(t, r.vals)
	}
	s := await(t, single)
	require.ErrorIs(t, s.err, boom)
	assert.Empty(t, s.val)
	assert.Zero(t, c.Len())
	assert.Zero(t, inFlight(c)) // the keys are not pinned by the failure

	b.releaseAll()
	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, DefaultTTL, b.fetch)
	require.NoError(t, err) // a failure is retried by the next miss
	assert.Equal(t, map[int]string{1: fetched(1, 2), 2: fetched(2, 2)}, got)
}

func Test_Cache_GetOrFetchMany_AFailedFetchFailsOnlyItsWaiters(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	boom := errors.New("boom")
	b.answer = func(call int, keys []int) (map[int]string, error) {
		if call == 1 {
			return nil, boom
		}
		return findAll(call, keys)
	}
	ctx := context.Background()

	a := goFetchMany(ctx, c, []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	bb := goFetchMany(ctx, c, []int{2, 3, 4}, DefaultTTL, b.fetch)
	require.Equal(t, 2, b.awaitStart(t))
	d := goFetchMany(ctx, c, []int{4}, DefaultTTL, b.fetch) // waits on B's fetch only
	waitMisses(t, c, 3+3+1)

	b.release(2)
	rd := await(t, d)
	require.NoError(t, rd.err)
	assert.Equal(t, map[int]string{4: fetched(4, 2)}, rd.vals)

	b.release(1)
	require.ErrorIs(t, await(t, a).err, boom)
	rb := await(t, bb)
	require.ErrorIs(t, rb.err, boom) // B waited on A's keys: A's error, and none of B's values
	assert.Nil(t, rb.vals)
	assert.True(t, c.Has(4)) // B's own fetch did succeed
	assert.False(t, c.Has(1))
	assert.False(t, c.Has(2))
	assert.False(t, c.Has(3))
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_TheFirstFailingKeyInTheOrderAskedWins(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	errOne, errTwo := errors.New("fetch of 1"), errors.New("fetch of 2")
	b.answer = func(call int, _ []int) (map[int]string, error) {
		return nil, map[int]error{1: errOne, 2: errTwo}[call]
	}
	_, _ = c.GetOrFetchMany(cancelledCtx(), []int{1}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	_, _ = c.GetOrFetchMany(cancelledCtx(), []int{2}, DefaultTTL, b.fetch)
	require.Equal(t, 2, b.awaitStart(t))

	twoFirst := goFetchMany(context.Background(), c, []int{2, 1}, DefaultTTL, b.fetch)
	oneFirst := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
	waitMisses(t, c, 1+1+2+2)

	b.release(1)
	require.ErrorIs(t, await(t, oneFirst).err, errOne) // returns without waiting on 2
	stillWaiting(t, twoFirst)                          // 2 comes first for it, and is still in flight
	b.release(2)
	require.ErrorIs(t, await(t, twoFirst).err, errTwo)
	assert.Len(t, b.calls(), 2)
}

func Test_Cache_GetOrFetchMany_PanicBecomesAnErrorForEveryWaiter(t *testing.T) {
	c := New[int, string]()
	c.Set(1, "cached", DefaultTTL)
	b := newBatchFetch(t)
	b.answer = func(int, []int) (map[int]string, error) { panic("kaboom") }

	starter := goFetchMany(context.Background(), c, []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	single := goFetch(context.Background(), c, 3, mustNotFetch(t))
	waitMisses(t, c, 2+1)
	b.release(1)

	r := await(t, starter)
	var perr *FetchPanicError
	require.ErrorAs(t, r.err, &perr)
	assert.Equal(t, []int{2, 3}, perr.Key) // the keys fetched, not the keys asked
	assert.Equal(t, "kaboom", perr.Value)
	assert.EqualError(t, r.err, "ttlcache: fetch of keys [2 3] panicked: kaboom")
	assert.Nil(t, r.vals)

	s := await(t, single) // a GetOrFetch that joined gets the same error
	var sperr *FetchPanicError
	require.ErrorAs(t, s.err, &sperr)
	assert.Same(t, perr, sperr)

	assert.False(t, c.Has(2))
	assert.False(t, c.Has(3))
	assert.True(t, c.Has(1))
	assert.Zero(t, inFlight(c))
}

/* ---- fencing: explicit writes during the fetch ---- */

func Test_Cache_GetOrFetchMany_ExplicitWritesFenceOnlyTheirKey(t *testing.T) {
	explicit := "explicit"
	writes := map[string]struct {
		write func(c *Cache[int, string])
		want2 *string // what the cache holds for 2 afterwards; nil: nothing
	}{
		"Set":          {func(c *Cache[int, string]) { c.Set(2, explicit, DefaultTTL) }, &explicit},
		"GetOrSet":     {func(c *Cache[int, string]) { c.GetOrSet(2, explicit) }, &explicit},
		"GetOrSetFunc": {func(c *Cache[int, string]) { c.GetOrSetFunc(2, func() string { return explicit }) }, &explicit},
		"Delete":       {func(c *Cache[int, string]) { c.Delete(2) }, nil},
		"GetAndDelete": {func(c *Cache[int, string]) { c.GetAndDelete(2) }, nil},
	}
	for name, tc := range writes {
		t.Run(name, func(t *testing.T) {
			c := New[int, string]()
			b := newBatchFetch(t)
			res := goFetchMany(context.Background(), c, []int{1, 2, 3}, DefaultTTL, b.fetch)
			require.Equal(t, 1, b.awaitStart(t))
			waitMisses(t, c, 3)

			tc.write(c)
			b.release(1)

			r := await(t, res) // the waiters still get what they asked for...
			require.NoError(t, r.err)
			assert.Equal(t, map[int]string{1: fetched(1, 1), 2: fetched(2, 1), 3: fetched(3, 1)}, r.vals)
			v2, ok := cached(c, 2) // ...but the fenced key keeps the write
			if tc.want2 == nil {
				assert.False(t, ok, "2 holds %q", v2)
			} else {
				assert.Equal(t, *tc.want2, v2)
			}
			v1, _ := cached(c, 1) // and the other keys of the fetch are cached
			v3, _ := cached(c, 3)
			assert.Equal(t, fetched(1, 1), v1)
			assert.Equal(t, fetched(3, 1), v3)
			assert.Zero(t, inFlight(c))
		})
	}
}

func Test_Cache_GetOrFetchMany_AFencedKeyIsFetchedAnew(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	ctx := context.Background()

	old := goFetchMany(ctx, c, []int{1, 2, 3}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	waitMisses(t, c, 3)
	c.Delete(2)

	fresh := goFetchMany(ctx, c, []int{2, 3}, DefaultTTL, b.fetch) // does not join the fenced fetch for 2
	require.Equal(t, 2, b.awaitStart(t))
	waitMisses(t, c, 3+2)
	assert.Equal(t, [][]int{{1, 2, 3}, {2}}, b.calls()) // 3 is still joined

	b.release(1)
	require.NoError(t, await(t, old).err)
	// the old fetch landing does not unregister the new one: a new miss joins it
	single := goFetch(ctx, c, 2, mustNotFetch(t))
	waitMisses(t, c, 3+2+1)
	assert.Equal(t, 1, inFlight(c))
	assert.False(t, c.Has(2))

	b.release(2)
	f := await(t, fresh)
	require.NoError(t, f.err)
	assert.Equal(t, map[int]string{2: fetched(2, 2), 3: fetched(3, 1)}, f.vals)
	s := await(t, single)
	require.NoError(t, s.err)
	assert.Equal(t, fetched(2, 2), s.val)
	v, _ := cached(c, 2)
	assert.Equal(t, fetched(2, 2), v)
	assert.Len(t, b.calls(), 2)
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_DeleteAllFencesTheWholeFetch(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	res := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	waitMisses(t, c, 2)

	c.DeleteAll()
	assert.Zero(t, inFlight(c))
	b.release(1)

	r := await(t, res)
	require.NoError(t, r.err)
	assert.Len(t, r.vals, 2)
	assert.Zero(t, c.Len())
}

func Test_Cache_GetOrFetchMany_AFencedFetchThatFailsCachesNothing(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	boom := errors.New("boom")
	b.answer = func(int, []int) (map[int]string, error) { return nil, boom }
	res := goFetchMany(context.Background(), c, []int{1, 2}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	waitMisses(t, c, 2)

	c.Delete(1)
	b.release(1)
	require.ErrorIs(t, await(t, res).err, boom)
	assert.Zero(t, c.Len())
	assert.Zero(t, inFlight(c))
}

func Test_Cache_GetOrFetchMany_AFetchMayUseTheCache(t *testing.T) {
	c := New[int, string]()
	var nested map[int]string
	got, err := c.GetOrFetchMany(context.Background(), []int{1, 2}, DefaultTTL, func(ctx context.Context, keys []int) (map[int]string, error) {
		c.Set(1, "explicit", DefaultTTL) // writing its own key fences it
		var err error
		nested, err = c.GetOrFetchMany(ctx, []int{9}, DefaultTTL, func(_ context.Context, ks []int) (map[int]string, error) {
			return findAll(2, ks)
		})
		if err != nil {
			return nil, err
		}
		return findAll(1, keys)
	})
	require.NoError(t, err) // no deadlock: the fetch runs without the cache's lock
	assert.Equal(t, map[int]string{1: fetched(1, 1), 2: fetched(2, 1)}, got)
	assert.Equal(t, map[int]string{9: fetched(9, 2)}, nested)
	v1, _ := cached(c, 1)
	v2, _ := cached(c, 2)
	assert.Equal(t, "explicit", v1)
	assert.Equal(t, fetched(2, 1), v2)
	assert.True(t, c.Has(9))
}

/* ---- mixing GetOrFetch and GetOrFetchMany ---- */

func Test_Cache_GetOrFetchMany_JoinsAGetOrFetchInFlight(t *testing.T) {
	for name, fail := range map[string]bool{"succeeds": false, "fails": true} {
		t.Run(name, func(t *testing.T) {
			c := New[int, string]()
			b := newBatchFetch(t)
			boom := errors.New("boom")
			release := make(chan struct{})
			single := goFetch(context.Background(), c, 2, func(context.Context, int) (string, error) {
				<-release
				if fail {
					return "", boom
				}
				return "single", nil
			})
			waitMisses(t, c, 1)

			res := goFetchMany(context.Background(), c, []int{1, 2, 3}, DefaultTTL, b.fetch)
			require.Equal(t, 1, b.awaitStart(t))
			assert.Equal(t, [][]int{{1, 3}}, b.calls()) // 2 is not fetched again
			b.release(1)
			waitCached(t, c, 3)
			stillWaiting(t, res) // still waiting on the single fetch of 2

			close(release)
			r := await(t, res)
			s := await(t, single)
			if fail {
				require.ErrorIs(t, r.err, boom)
				require.ErrorIs(t, s.err, boom)
				assert.False(t, c.Has(2))
			} else {
				require.NoError(t, r.err)
				assert.Equal(t, map[int]string{1: fetched(1, 1), 2: "single", 3: fetched(3, 1)}, r.vals)
				assert.Equal(t, "single", s.val)
			}
			assert.True(t, c.Has(1)) // the batch's own keys are cached either way
			assert.True(t, c.Has(3))
			assert.Zero(t, inFlight(c))
		})
	}
}

func Test_Cache_GetOrFetch_JoinsAGetOrFetchManyInFlight(t *testing.T) {
	c := New[int, string]()
	b := newBatchFetch(t)
	b.answer = func(call int, _ []int) (map[int]string, error) { // finds 1, not 2
		return map[int]string{1: fetched(1, call)}, nil
	}
	_, _ = c.GetOrFetchMany(cancelledCtx(), []int{1, 2}, DefaultTTL, b.fetch)
	require.Equal(t, 1, b.awaitStart(t))
	found := goFetch(context.Background(), c, 1, mustNotFetch(t))
	notFound := goFetch(context.Background(), c, 2, mustNotFetch(t))
	waitMisses(t, c, 2+1+1)
	b.release(1)

	f := await(t, found)
	require.NoError(t, f.err)
	assert.Equal(t, fetched(1, 1), f.val)
	nf := await(t, notFound)
	require.ErrorIs(t, nf.err, ErrNotFound) // a FetchFunc never answers "not found": the batch did
	assert.Empty(t, nf.val)
	assert.False(t, c.Has(2))
	assert.Zero(t, inFlight(c))

	// not cached, so the next GetOrFetch runs its own fetch
	got, err := c.GetOrFetch(context.Background(), 2, DefaultTTL, func(context.Context, int) (string, error) {
		return "single", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "single", got)
	v, _ := cached(c, 2)
	assert.Equal(t, "single", v)
}

// A FetchFunc's zero value is a value: never ErrNotFound, for the caller
// that started the fetch nor for the ones that joined it.
func Test_Cache_GetOrFetch_ZeroValueIsNotErrNotFound(t *testing.T) {
	c := New[int, string]()
	release := make(chan struct{})
	zero := func(context.Context, int) (string, error) {
		<-release
		return "", nil
	}
	starter := goFetch(context.Background(), c, 1, zero)
	waitMisses(t, c, 1)
	joiner := goFetch(context.Background(), c, 1, mustNotFetch(t))
	batch := goFetchMany(context.Background(), c, []int{1}, DefaultTTL, func(context.Context, []int) (map[int]string, error) {
		t.Error("GetOrFetchMany fetched a key already in flight")
		return nil, nil
	})
	waitMisses(t, c, 3)
	close(release)

	for _, r := range []oneResult{await(t, starter), await(t, joiner)} {
		require.NoError(t, r.err)
		assert.Empty(t, r.val)
	}
	rb := await(t, batch)
	require.NoError(t, rb.err)
	assert.Equal(t, map[int]string{1: ""}, rb.vals)
	assert.True(t, c.Has(1))
}

/* ---- everything at once, for the race detector ---- */

// Callers of both kinds, explicit writes and wipes, all at once. With fencing,
// whatever a caller gets for k is a fetched or an explicit value of k, and no
// fetch is left registered once everyone is done.
func Test_Cache_GetOrFetchMany_MixedConcurrentUse(t *testing.T) {
	const keys, workers, ops = 16, 16, 200
	c := New[int, string]()
	fetchOne := func(_ context.Context, k int) (string, error) {
		time.Sleep(50 * time.Microsecond)
		return fmt.Sprint("v", k), nil
	}
	fetchMany := func(_ context.Context, ks []int) (map[int]string, error) {
		time.Sleep(50 * time.Microsecond)
		vals := make(map[int]string, len(ks))
		for _, k := range ks {
			vals[k] = fmt.Sprint("v", k)
		}
		return vals, nil
	}
	valid := func(k int, v string) bool { return v == fmt.Sprint("v", k) || v == fmt.Sprint("x", k) }

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 7))
			for range ops {
				k := rng.IntN(keys)
				switch op := rng.IntN(10); {
				case op < 4:
					ks := []int{k, rng.IntN(keys), rng.IntN(keys)}
					got, err := c.GetOrFetchMany(context.Background(), ks, DefaultTTL, fetchMany)
					if !assert.NoError(t, err) {
						return
					}
					for _, key := range ks {
						assert.True(t, valid(key, got[key]), "key %d: %q", key, got[key])
					}
				case op < 7:
					v, err := c.GetOrFetch(context.Background(), k, DefaultTTL, fetchOne)
					if !assert.NoError(t, err) {
						return
					}
					assert.True(t, valid(k, v), "key %d: %q", k, v)
				case op < 8:
					c.Set(k, fmt.Sprint("x", k), DefaultTTL)
				case op < 9:
					c.Delete(k)
				default:
					if rng.IntN(10) == 0 {
						c.DeleteAll()
					}
				}
			}
		})
	}
	wg.Wait()

	waitNoFetches(t, c)
	for k, item := range c.Items() {
		assert.True(t, valid(k, item.Value()), "key %d holds %q", k, item.Value())
	}
}
