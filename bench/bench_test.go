package bench

import (
	"testing"
	"time"

	"github.com/ReneKroon/ttlcache"
)

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		cache.Set(string(n), "value")
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	for n := 0; n < b.N; n++ {
		cache.Set(string(n), "value")
	}
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		cache.SetWithTTL(string(n), "value", time.Duration(50*time.Millisecond))
	}
}
