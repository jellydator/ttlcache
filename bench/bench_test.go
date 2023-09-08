package bench

import (
	"fmt"
	"testing"
	"time"

	ttlcache "github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/assert"
)

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := ttlcache.New[string, string]()

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value", ttlcache.NoTTL)
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](50 * time.Millisecond),
	)

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value", ttlcache.DefaultTTL)
	}
}

func BenchmarkCacheModifyOrSetFunc(b *testing.B) {
	const (
		key = "key"
	)

	b.Run("With ModifyOrSetFunc", func(b *testing.B) {
		cache := ttlcache.New[string, []int](
			ttlcache.WithTTL[string, []int](50 * time.Second),
		)
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			n := n
			cache.ModifyOrSetFunc(key, func(value []int) []int {
				return append(value, n)
			}, func() []int {
				return append(make([]int, 0, 100), n)
			}, ttlcache.DefaultTTL)
		}

		outcome := cache.Get(key)
		assert.Equal(b, b.N, len(outcome.Value()))
	})

	b.Run("Without ModifyOrSetFunc", func(b *testing.B) {
		cache := ttlcache.New[string, []int](
			ttlcache.WithTTL[string, []int](50 * time.Second),
		)
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			n := n
			item, found := cache.GetOrSet(key, append(make([]int, 0, 100), n))
			if found {
				cache.Set(key, append(item.Value(), n), ttlcache.DefaultTTL)
			}
		}
		outcome := cache.Get(key)
		assert.Equal(b, b.N, len(outcome.Value()))
	})
}
