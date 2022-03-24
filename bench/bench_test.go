package bench

import (
	"fmt"
	"testing"
	"time"

	ttlcache "github.com/jellydator/ttlcache/v2"
)

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value")
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	for n := 0; n < b.N; n++ {
		cache.Set(fmt.Sprint(n%1000000), "value")
	}
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		cache.SetWithTTL(fmt.Sprint(n%1000000), "value", time.Duration(50*time.Millisecond))
	}
}
