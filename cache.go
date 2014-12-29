package ttlcache

import (
	"reflect"
	"sync"
	"time"
)

// This func is used as a callback on item expiration
type ExpireCallback func(key string, value interface{})

// Cache is a synchronized map of items that auto-expire once stale
type Cache struct {
	mutex          sync.RWMutex
	ttl            time.Duration
	items          map[string]*Item
	expireCallback ExpireCallback
	expireChannel  chan (<-chan time.Time)
}

/*
agrupar todos os items por data de expiracao em um array, qnd ele expirar, percorrer o array e ver quem vai ser expirado
*/

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {
	cache := &Cache{items: map[string]*Item{}}
	go cache.processExpirations()
	return cache
}

// Set the default timeout and initialize the previous items with empty timeout with the new
func (cache *Cache) SetTimeout(ttl time.Duration) {
	cache.ttl = ttl
	initializeEmptyItemsTTL(cache)
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (data interface{}, found bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item, exists := cache.items[key]
	if exists {
		item.touch()
	}

	return item.data, exists
}

// Set is a thread-safe way to add new items to the map
func (cache *Cache) Set(key string, data interface{}) {
	cache.SetWithTTL(key, data, cache.ttl)
}

// Set is a thread-safe way to add new items to the map with individual ttl
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item := &Item{data: data, ttl: ttl}
	item.touch()
	cache.items[key] = item
	cache.expireChannel <- item.expire
}

// Count returns the number of items in the cache
func (cache *Cache) Count() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return len(cache.items)
}

func (cache *Cache) SetExpireCallback(callback ExpireCallback) {
	cache.expireCallback = callback
}

func (cache *Cache) processExpirations() {
	ttlCases := make([]reflect.SelectCase, 1)
	ttlCases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(cache.expireChannel)}

	for {
		chosen, value, ok := reflect.Select(ttlCases)

		if chosen == 0 {
			ttlCases = append(ttlCases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(value)})
		} else {
			// espirar o item

			if !ok {
				//cache.mutex.Lock()
				//fmt.Printf("%T\n", ttlCases[chosen], ttlCases[chosen].Chan, value)
				//close(reflect.ValueOf(ttlCases[chosen].Chan)
				ttlCases = append(ttlCases[:len(ttlCases)], ttlCases[len(ttlCases)+1:]...)
				//cache.mutex.Unlock()

				// fechar o canal, tirar o ttlCases
			}
		}
	}
}
