package bench

import (
	"fmt"
	"testing"
	"time"

	ttlcache "github.com/asgarciap/ttlcache/v2"
)

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		err := cache.Set(fmt.Sprint(n%1000000), "value")
		if err != nil {
			b.Errorf("Error when inserting item %v", err)
		}
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	for n := 0; n < b.N; n++ {
		err := cache.Set(fmt.Sprint(n%1000000), "value")
		if err != nil {
			b.Errorf("Error when inserting item %v", err)
		}
	}
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	cache := ttlcache.NewCache()
	defer cache.Close()

	for n := 0; n < b.N; n++ {
		err := cache.SetWithTTL(fmt.Sprint(n%1000000), "value", time.Duration(50*time.Millisecond))
		if err != nil {
			b.Errorf("Error when inserting item %v", err)
		}
	}
}
