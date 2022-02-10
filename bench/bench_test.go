package bench

import (
	"fmt"
	"testing"
	"time"

	ttlcache "github.com/ReneKroon/ttlcache/v3"
)

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := ttlcache.NewCache[string, string]()

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value", ttlcache.NoTTL)
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := ttlcache.NewCache[string, string](
		ttlcache.WithTTL(50 * time.Millisecond),
	)

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value", ttlcache.DefaultTTL)
	}
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	cache := ttlcache.NewCache[string, string]()

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value", 50*time.Millisecond)
	}
}
