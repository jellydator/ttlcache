package ttlcache

import (
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"golang.org/x/sync/singleflight"
)

type suppressedDisplayKey struct {
	id int
}

func (suppressedDisplayKey) String() string   { return "same" }
func (suppressedDisplayKey) GoString() string { return "same" }

func TestSuppressedLoaderKeyIdentity(t *testing.T) {
	type pair struct{ first, second string }
	a, b := &suppressedDisplayKey{1}, &suppressedDisplayKey{2}
	for _, tc := range []struct {
		name     string
		first    any
		second   any
		firstNil bool
		same     bool
	}{
		{name: "interface types", first: 1, second: "1"},
		{name: "composite display", first: pair{"a b", "c"}, second: pair{"a", "b c"}},
		{name: "custom display", first: suppressedDisplayKey{1}, second: suppressedDisplayKey{2}},
		{name: "pointer identity", first: a, second: b},
		{name: "nil collision", first: 1, second: "1", firstNil: true},
		{name: "equal key", first: "equal", second: "equal", same: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cache := New[any, string]()
				started := make(chan struct{})
				release := make(chan struct{})
				var calls atomic.Int32
				loader := NewSuppressedLoader[any, string](
					LoaderFunc[any, string](func(_ *Cache[any, string], key any) *Item[any, string] {
						if calls.Add(1) == 1 {
							close(started)
						}
						<-release
						if tc.firstNil && key == tc.first {
							return nil
						}
						return NewItemWithOpts(key, "loaded", NoTTL)
					}), nil,
				)
				first := make(chan *Item[any, string], 1)
				second := make(chan *Item[any, string], 1)
				go func() { first <- loader.Load(cache, tc.first) }()
				<-started
				go func() { second <- loader.Load(cache, tc.second) }()
				synctest.Wait()
				close(release)
				gotFirst, gotSecond := <-first, <-second
				if tc.firstNil {
					if gotFirst != nil {
						t.Fatal("first result should be nil")
					}
				} else if gotFirst == nil || gotFirst.Key() != tc.first {
					t.Fatalf("first result has the wrong key: %v", gotFirst)
				}
				if gotSecond == nil || gotSecond.Key() != tc.second {
					t.Errorf("second result belongs to a different key: %v", gotSecond)
				}
				wantCalls := int32(2)
				if tc.same {
					wantCalls = 1
					if gotFirst != gotSecond {
						t.Error("equal keys should share the same result")
					}
				}
				if calls.Load() != wantCalls {
					t.Errorf("loader calls = %d, want %d", calls.Load(), wantCalls)
				}
			})
		})
	}
}

func TestSuppressedLoaderSharedGroup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := New[string, string]()
		group := new(singleflight.Group)
		started := make(chan struct{})
		release := make(chan struct{})
		var calls atomic.Int32
		load := LoaderFunc[string, string](func(_ *Cache[string, string], key string) *Item[string, string] {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return NewItemWithOpts(key, "loaded", NoTTL)
		})
		a := NewSuppressedLoader(load, group)
		b := NewSuppressedLoader(load, group)
		first := make(chan *Item[string, string], 1)
		second := make(chan *Item[string, string], 1)
		go func() { first <- a.Load(cache, "equal") }()
		<-started
		go func() { second <- b.Load(cache, "equal") }()
		synctest.Wait()
		close(release)
		if x, y := <-first, <-second; x != y {
			t.Error("equal keys using a supplied group should share a result across loaders")
		}
		if calls.Load() != 1 {
			t.Errorf("loader calls = %d, want 1", calls.Load())
		}
	})
}

func TestSuppressedLoaderRepeatedLoads(t *testing.T) {
	for _, nilResult := range []bool{false, true} {
		var calls int
		cache := New[string, string]()
		loader := NewSuppressedLoader(
			LoaderFunc[string, string](func(_ *Cache[string, string], key string) *Item[string, string] {
				calls++
				if nilResult {
					return nil
				}
				return NewItemWithOpts(key, "loaded", NoTTL)
			}), nil,
		)
		for range 2 {
			item := loader.Load(cache, "key")
			if nilResult != (item == nil) {
				t.Fatal("unexpected nil result")
			}
		}
		if calls != 2 {
			t.Errorf("completed loads must not cache results: calls=%d", calls)
		}
	}
}

func TestSuppressedLoaderNonreflexiveKey(t *testing.T) {
	cache := New[float64, string]()
	loader := NewSuppressedLoader(
		LoaderFunc[float64, string](func(_ *Cache[float64, string], key float64) *Item[float64, string] {
			return NewItemWithOpts(key, "loaded", NoTTL)
		}), nil,
	)
	item := loader.Load(cache, math.NaN())
	if item == nil || !math.IsNaN(item.Key()) {
		t.Fatal("a nonreflexive key should still load normally")
	}
}
