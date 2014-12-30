package ttlcache

import (
	"testing"
	"time"
)

func TestWithIndividualTTL(t *testing.T) {
	ttl := time.Duration(1 * time.Second)
	cache := NewCache()
	cache.SetTimeout(ttl, ttl)
	cache.SetWithTTL("key", "value", ttl)

	<-time.After(2 * time.Second)

	if cache.Count() > 0 {
		t.Error("Key didn't expire")
	}
}

func TestGet(t *testing.T) {
	ttl := time.Duration(1 * time.Second)
	cache := NewCache()
	cache.SetTimeout(ttl, ttl)

	data, exists := cache.Get("hello")
	if exists || data != nil {
		t.Errorf("Expected empty cache to return no data")
	}

	cache.Set("hello", "world")
	data, exists = cache.Get("hello")
	if !exists {
		t.Errorf("Expected cache to return data for `hello`")
	}
	if data != "world" {
		t.Errorf("Expected cache to return `world` for `hello`")
	}
}

func TestCallbackFunction(t *testing.T) {
	expired := false
	ttl := time.Duration(1 * time.Second)
	cache := NewCache()
	cache.SetTimeout(ttl, ttl)
	cache.SetExpireCallback(func(key string, value interface{}) {
		expired = true
	})
	cache.Set("testEviction", "should expire")
	<-time.After(2 * time.Second)
	if !expired {
		t.Errorf("Expected cache to expire")
	}
}

func TestExpiration(t *testing.T) {
	ttl := time.Duration(1 * time.Second)
	cache := NewCache()
	cache.SetTimeout(ttl, ttl)
	cache.Set("x", "1")
	cache.Set("y", "z")
	cache.Set("z", "3")
	count := cache.Count()

	if count != 3 {
		t.Errorf("Expected cache to contain 3 items")
	}

	<-time.After(500 * time.Millisecond)
	cache.mutex.Lock()
	cache.items["y"].touch()
	item, exists := cache.items["x"]
	cache.mutex.Unlock()
	if !exists || item.data != "1" || item.expired() {
		t.Errorf("Expected `x` to not have expired after 200ms")
	}

	<-time.After(time.Second)
	cache.mutex.RLock()
	_, exists = cache.items["x"]
	if exists {
		t.Errorf("Expected `x` to have expired")
	}
	_, exists = cache.items["z"]
	if exists {
		t.Errorf("Expected `z` to have expired")
	}
	_, exists = cache.items["y"]
	if !exists {
		t.Errorf("Expected `y` to not have expired")
	}
	cache.mutex.RUnlock()

	count = cache.Count()
	if count != 1 {
		t.Errorf("Expected cache to contain 1 item")
	}

	<-time.After(600 * time.Millisecond)
	cache.mutex.RLock()
	_, exists = cache.items["y"]
	if exists {
		t.Errorf("Expected `y` to have expired")
	}
	cache.mutex.RUnlock()

	count = cache.Count()
	if count != 0 {
		t.Errorf("Expected cache to be empty")
	}
}
