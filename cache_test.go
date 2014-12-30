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

/*
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
}*/

func TestExpiration(t *testing.T) {
	ttl := time.Duration(1 * time.Second)
	cache := NewCache()
	cache.SetTimeout(ttl, ttl)
	cache.SetWithTTL("x", "1", ttl)

	count := cache.Count()
	if count != 1 {
		t.Errorf("Expected cache to contain 1 item")
	}

	<-time.After(2 * time.Second)

	count = cache.Count()
	if count != 0 {
		t.Errorf("Expected cache to by empty")
	}

	cache.SetWithTTL("x", "1", ttl)
	<-time.After(500 * time.Millisecond)
	cache.Get("x")
	<-time.After(500 * time.Millisecond)

	count = cache.Count()
	if count != 1 {
		t.Errorf("Expected cache to contain 1 item")
	}
}
