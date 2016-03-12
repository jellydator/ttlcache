package ttlcache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheIndividualExpirationBiggerThanGlobal(t *testing.T) {
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	<-time.After(150 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, false, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpirationByGlobal(t *testing.T) {
	cache := NewCache()
	cache.Set("key", "value")
	<-time.After(50 * time.Millisecond)
	data, exists := cache.Get("key")
	assert.Equal(t, exists, true, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	cache.SetTTL(time.Duration(50 * time.Millisecond))
	data, exists = cache.Get("key")
	assert.Equal(t, exists, true, "Expected item to exist in cache")
	assert.Equal(t, data.(string), "value", "Expected item to have 'value' in value")

	<-time.After(100 * time.Millisecond)
	data, exists = cache.Get("key")
	assert.Equal(t, exists, false, "Expected item to not exist")
	assert.Nil(t, data, "Expected item to be nil")
}

func TestCacheGlobalExpiration(t *testing.T) {
	cache := NewCache()
	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_1", "value")
	cache.Set("key_2", "value")
	<-time.After(200 * time.Millisecond)
	assert.Equal(t, cache.Count(), 0, "Cache should be empty")
	assert.Equal(t, cache.priorityQueue.Len(), 0, "PriorityQueue should be empty")
}

func TestCacheMixedExpirations(t *testing.T) {
	cache := NewCache()
	cache.Set("key_1", "value")
	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(200 * time.Millisecond)
	assert.Equal(t, cache.Count(), 1, "Cache should have only 1 item")
}

func TestCacheIndividualExpiration(t *testing.T) {
	cache := NewCache()
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key2", "value", time.Duration(100*time.Millisecond))
	cache.SetWithTTL("key3", "value", time.Duration(100*time.Millisecond))
	<-time.After(50 * time.Millisecond)
	assert.Equal(t, cache.Count(), 3, "Should have 3 elements in cache")
	<-time.After(160 * time.Millisecond)
	assert.Equal(t, cache.Count(), 0, "Cache should be empty")

	cache.SetWithTTL("key4", "value", time.Duration(50*time.Millisecond))
	<-time.After(100 * time.Millisecond)
	<-time.After(100 * time.Millisecond)
	assert.Equal(t, cache.Count(), 0, "Cache should be empty")
}

func TestCacheGet(t *testing.T) {
	cache := NewCache()
	data, exists := cache.Get("hello")
	assert.Equal(t, exists, false, "Expected empty cache to return no data")
	assert.Nil(t, data, "Expected data to be empty")

	cache.Set("hello", "world")
	data, exists = cache.Get("hello")
	assert.NotNil(t, data, "Expected data to be not nil")
	assert.Equal(t, exists, true, "Expected data to exist")
	assert.Equal(t, (data.(string)), "world", "Expected data content to be 'world'")
}

func TestCacheExpirationCallbackFunction(t *testing.T) {
	expiredCount := 0
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetExpirationCallback(func(key string, value interface{}) {
		expiredCount = expiredCount + 1
	})
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(110 * time.Millisecond)
	assert.Equal(t, expiredCount, 2, "Expected 2 items to be expired")
}

func TestCacheCheckExpirationCallbackFunction(t *testing.T) {
	expiredCount := 0
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetCheckExpirationCallback(func(key string, value interface{}) bool {
		if key == "key2" {
			return true
		}
		return false
	})
	cache.SetExpirationCallback(func(key string, value interface{}) {
		expiredCount = expiredCount + 1
	})
	cache.Set("key", "value")
	cache.Set("key2", "value")
	<-time.After(110 * time.Millisecond)
	assert.Equal(t, expiredCount, 1, "Expected 1 item to be expired")
}

func TestCacheNewItemCallbackFunction(t *testing.T) {
	newItemCount := 0
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetNewItemCallback(func(key string, value interface{}) {
		newItemCount = newItemCount + 1
	})
	cache.Set("key", "value")
	cache.Set("key2", "value")
	cache.Set("key", "value")
	<-time.After(110 * time.Millisecond)
	assert.Equal(t, newItemCount, 2, "Expected only 2 new items")
}

func TestCacheRemove(t *testing.T) {
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(100*time.Millisecond))
	cache.Set("key_2", "value")
	<-time.After(70 * time.Millisecond)
	removeKey := cache.Remove("key")
	removeKey2 := cache.Remove("key_2")
	assert.Equal(t, removeKey, true, "Expected 'key' to be removed from cache")
	assert.Equal(t, removeKey2, false, "Expected 'key_2' to already be expired from cache")
}

func TestCacheSetWithTTLExistItem(t *testing.T) {
	cache := NewCache()
	cache.SetTTL(time.Duration(100 * time.Millisecond))
	cache.SetWithTTL("key", "value", time.Duration(50*time.Millisecond))
	<-time.After(30 * time.Millisecond)
	cache.SetWithTTL("key", "value2", time.Duration(50*time.Millisecond))
	data, exists := cache.Get("key")
	assert.Equal(t, exists, true, "Expected 'key' to exist")
	assert.Equal(t, data.(string), "value2", "Expected 'data' to have value 'value2'")
}

func BenchmarkCacheSetWithoutTTL(b *testing.B) {
	cache := NewCache()
	for n := 0; n < b.N; n++ {
		cache.Set(string(n), "value")
	}
}

func BenchmarkCacheSetWithGlobalTTL(b *testing.B) {
	cache := NewCache()
	cache.SetTTL(time.Duration(50 * time.Millisecond))
	for n := 0; n < b.N; n++ {
		cache.Set(string(n), "value")
	}
}

func BenchmarkCacheSetWithTTL(b *testing.B) {
	cache := NewCache()
	for n := 0; n < b.N; n++ {
		cache.SetWithTTL(string(n), "value", time.Duration(50*time.Millisecond))
	}
}
