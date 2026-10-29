package ttlcache

import (
	"math"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"golang.org/x/sync/singleflight"
)

func assertNoSuppressedFlights(t *testing.T, group *singleflight.Group) {
	t.Helper()
	suppressedLoaderFlights.Lock()
	defer suppressedLoaderFlights.Unlock()
	for key := range suppressedLoaderFlights.entries {
		if key.group == group {
			t.Error("completed load retained a registry entry")
		}
	}
}

func TestSuppressedLoaderEqualityBoundaries(t *testing.T) {
	type wrapped struct{ Value any }
	var typedNil *suppressedDisplayKey
	for _, tc := range []struct {
		name  string
		key   any
		calls int32
	}{
		{"nil interface", nil, 1},
		{"typed nil pointer", typedNil, 1},
		{"typed nil slice", []int(nil), 2},
		{"slice", []int{1}, 2},
		{"nested incomparable interface", wrapped{[]int{1}}, 2},
		{"NaN", math.NaN(), 2},
		{"nested NaN", wrapped{math.NaN()}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cache := New[any, string]()
				group := new(singleflight.Group)
				started, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				loader := NewSuppressedLoader(
					LoaderFunc[any, string](func(_ *Cache[any, string], key any) *Item[any, string] {
						if calls.Add(1) == 1 {
							close(started)
						}
						<-release
						return NewItemWithOpts(key, "loaded", NoTTL)
					}), group,
				)
				done := make(chan *Item[any, string], 2)
				go func() { done <- loader.Load(cache, tc.key) }()
				<-started
				go func() { done <- loader.Load(cache, tc.key) }()
				synctest.Wait()
				close(release)
				if x, y := <-done, <-done; x == nil || y == nil {
					t.Fatal("loads should return their items")
				} else if tc.calls == 1 && x != y {
					t.Error("equal keys should share a result")
				}
				if calls.Load() != tc.calls {
					t.Errorf("loader calls = %d, want %d", calls.Load(), tc.calls)
				}
				assertNoSuppressedFlights(t, group)
			})
		})
	}
}

func TestSuppressedLoaderSignedZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := New[float64, string]()
		group := new(singleflight.Group)
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		loader := NewSuppressedLoader(
			LoaderFunc[float64, string](func(_ *Cache[float64, string], key float64) *Item[float64, string] {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return NewItemWithOpts(key, "loaded", NoTTL)
			}), group,
		)
		done := make(chan *Item[float64, string], 2)
		go func() { done <- loader.Load(cache, 0) }()
		<-started
		go func() { done <- loader.Load(cache, math.Copysign(0, -1)) }()
		synctest.Wait()
		close(release)
		if a, b := <-done, <-done; a != b || calls.Load() != 1 {
			t.Error("positive and negative zero are equal keys and should share a result")
		}
		assertNoSuppressedFlights(t, group)
	})
}

func TestSuppressedLoaderSharedGroupValueTypes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		group := new(singleflight.Group)
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		stringLoader := NewSuppressedLoader(
			LoaderFunc[string, string](func(_ *Cache[string, string], key string) *Item[string, string] {
				calls.Add(1)
				close(started)
				<-release
				return NewItemWithOpts(key, "text", NoTTL)
			}), group,
		)
		intLoader := NewSuppressedLoader(
			LoaderFunc[string, int](func(_ *Cache[string, int], key string) *Item[string, int] {
				calls.Add(1)
				<-release
				return NewItemWithOpts(key, 42, NoTTL)
			}), group,
		)
		text := make(chan *Item[string, string], 1)
		number := make(chan *Item[string, int], 1)
		go func() { text <- stringLoader.Load(New[string, string](), "same") }()
		<-started
		go func() { number <- intLoader.Load(New[string, int](), "same") }()
		synctest.Wait()
		close(release)
		if (<-text).Value() != "text" || (<-number).Value() != 42 || calls.Load() != 2 {
			t.Error("different item types must not share a result")
		}
		assertNoSuppressedFlights(t, group)
	})
}

func TestSuppressedLoaderSeparateGroups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := New[string, string]()
		firstGroup, secondGroup := new(singleflight.Group), new(singleflight.Group)
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		load := LoaderFunc[string, string](func(_ *Cache[string, string], key string) *Item[string, string] {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return NewItemWithOpts(key, "loaded", NoTTL)
		})
		first, second := NewSuppressedLoader(load, firstGroup), NewSuppressedLoader(load, secondGroup)
		done := make(chan *Item[string, string], 2)
		go func() { done <- first.Load(cache, "same") }()
		<-started
		go func() { done <- second.Load(cache, "same") }()
		synctest.Wait()
		close(release)
		<-done
		<-done
		if calls.Load() != 2 {
			t.Error("different groups must not share a load")
		}
		assertNoSuppressedFlights(t, firstGroup)
		assertNoSuppressedFlights(t, secondGroup)
	})
}

func TestSuppressedLoaderAbnormalExitCleanup(t *testing.T) {
	for _, mode := range []string{"panic", "Goexit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cache := New[string, string]()
				group := new(singleflight.Group)
				started, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				loader := NewSuppressedLoader(
					LoaderFunc[string, string](func(_ *Cache[string, string], key string) *Item[string, string] {
						if calls.Add(1) == 1 {
							close(started)
							<-release
							if mode == "panic" {
								panic("expected loader panic")
							}
							runtime.Goexit()
						}
						return NewItemWithOpts(key, "loaded", NoTTL)
					}), group,
				)
				done := make(chan struct{}, 2)
				run := func() {
					defer func() {
						recovered := recover()
						if mode == "panic" && recovered == nil {
							t.Error("the loader panic should propagate to each waiter")
						}
						if mode == "Goexit" && recovered != nil {
							t.Error("Goexit should not become a panic")
						}
						done <- struct{}{}
					}()
					loader.Load(cache, "same")
					t.Error("an interrupted load should not return normally")
				}
				go run()
				<-started
				go run()
				synctest.Wait()
				close(release)
				<-done
				<-done
				assertNoSuppressedFlights(t, group)
				if calls.Load() != 1 {
					t.Error("equal-key waiters should share the failing load")
				}
				if item := loader.Load(cache, "same"); item == nil || item.Value() != "loaded" {
					t.Error("a later load should run after the failed flight")
				}
				assertNoSuppressedFlights(t, group)
			})
		})
	}
}

func TestSuppressedLoaderUnsharedPanic(t *testing.T) {
	for _, key := range []any{math.NaN(), []int{1}} {
		cache := New[any, string]()
		group := new(singleflight.Group)
		loader := NewSuppressedLoader(
			LoaderFunc[any, string](func(_ *Cache[any, string], key any) *Item[any, string] {
				panic("expected loader panic")
			}), group,
		)
		done := make(chan any, 1)
		go func() {
			defer func() { done <- recover() }()
			loader.Load(cache, key)
		}()
		// singleflight wraps a loader panic in an error. Even an unshared
		// load should take that same execution path.
		if _, ok := (<-done).(error); !ok {
			t.Error("unshared load changed the singleflight panic wrapper")
		}
		assertNoSuppressedFlights(t, group)
	}
}
