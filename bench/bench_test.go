package bench

import (
	"fmt"
	"testing"
	"time"

	ttlcache "github.com/jellydator/ttlcache/v3"
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

func BenchmarkCacheGetOrSet(b *testing.B) {
	const (
		key = "key"
	)

	b.Run("Item Present", func(b *testing.B) {
		b.Run("With GetOrSet", func(b *testing.B) {
			cache := ttlcache.New[string, []string](
				ttlcache.WithTTL[string, []string](10 * time.Second),
			)
			cache.Set(key, []string{"1", "2"}, ttlcache.DefaultTTL)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				cache.GetOrSet(key, make([]string, 0, 100))
			}
		})
		b.Run("With GetOrSetFunc", func(b *testing.B) {
			cache := ttlcache.New[string, []string](
				ttlcache.WithTTL[string, []string](10 * time.Second),
			)
			cache.Set(key, []string{"1", "2"}, ttlcache.DefaultTTL)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				cache.GetOrSetFunc(key, func() []string { return make([]string, 0, 100) })
			}
		})
	})

	b.Run("Item Missing", func(b *testing.B) {
		b.Run("With GetOrSet", func(b *testing.B) {
			cache := ttlcache.New[int, []string](
				ttlcache.WithTTL[int, []string](10 * time.Second),
			)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				cache.GetOrSet(n, make([]string, 0, 100))
			}
		})
		b.Run("With GetOrSetFunc", func(b *testing.B) {
			cache := ttlcache.New[int, []string](
				ttlcache.WithTTL[int, []string](10 * time.Second),
			)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				cache.GetOrSetFunc(n, func() []string {
					return make([]string, 0, 100)
				})
			}
		})
	})
}
