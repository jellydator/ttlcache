package ttlcache

import (
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	cache := &Cache{
		ttl:   time.Second,
		items: map[string]*Item{},
	}

	data, exists := cache.Get("hello")
	if exists || data != "" {
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

func TestExpiration(t *testing.T) {
	cache := &Cache{
		ttl:   time.Second,
		items: map[string]*Item{},
	}

	cache.Set("x", "1")
	cache.Set("y", "z")
	cache.Set("z", "3")
	cache.startCleanupTimer()

	<-time.After(500 * time.Millisecond)
	item, exists := cache.items["x"]
	if !exists || item.data != "1" || item.expired() {
		t.Errorf("Expected `x` to not have expired after 200ms")
	}

	cache.items["y"].touch(time.Second)
	<-time.After(time.Second)

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
	<-time.After(600 * time.Millisecond)
	_, exists = cache.items["y"]
	if exists {
		t.Errorf("Expected `y` to have expired")
	}
}
