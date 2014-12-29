package ttlcache

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

// This func is used as a callback on item expiration
type ExpireCallback func(key string, value interface{})

// Cache is a synchronized map of items that auto-expire once stale
type Cache struct {
	mutex           sync.RWMutex
	ttl             time.Duration
	itemsWithTTL    map[string]*Item
	itemsWithoutTTL map[string]*Item
	expireCallback  ExpireCallback
	expireChannel   chan (<-chan time.Time)
}

// NewCache is a helper to create instance of the Cache struct
func NewCache(duration time.Duration) *Cache {
	cache := &Cache{
		ttl:   duration,
		items: map[string]*Item{},
	}
	go cache.processExpirations()
	return cache
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (data interface{}, found bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	get := func(items map[string]*Item, key string, ttl time.Duration) {
		item, exists := items[key]

		if exists {
			item.touch(ttl)
		}

		return item.data, true
	}

	item, exists := get(cache.itemsWithTTL, key, ttl)
	if exists {
		return item.data, exists
	}

	item, exists = get(cache.itemsWithoutTTL, key, item.ttl)
	if exists {
		return item.data, exists
	}

	return item.data, false
}

// Set is a thread-safe way to add new items to the map
func (cache *Cache) Set(key string, data interface{}) {
	SetWithTTL(key, data, cache.ttl)
}

// Set is a thread-safe way to add new items to the map with individual ttl
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item := &Item{data: data, ttl: ttl}
	item.touch(item.ttl)
	cache.expireChannel <- item.expire
	cache.items[key] = item
}

// Count returns the number of items in the cache
func (cache *Cache) Count() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	return len(cache.itemsWithTTL) + len(cache.itemsWithoutTTL)
}

func (cache *Cache) SetExpireCallback(callback ExpireCallback) {
	cache.expireCallback = callback
	// agora que o callback foi setado, algo deve ser iniciado para que isso faca sentido....
}

func (cache *Cache) processExpirations() {
	ttlCases := make([]reflect.SelectCase, 1)
	ttlCases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ttlChannel)}

	for {
		chosen, value, ok := reflect.Select(ttlCases)

		fmt.Println(chosen, value)
		if chosen == 0 {
			if !ok {
				break
			}

			cache.mutex.Lock()
			ttlCases = append(ttlCases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(<-ttlChannel)})
			cache.mutex.Unlock()
		} else {
			// espirar o item

			if !ok {
				//cache.mutex.Lock()
				fmt.Printf("%T\n", ttlCases[chosen], ttlCases[chosen].Chan, value)
				//close(reflect.ValueOf(ttlCases[chosen].Chan)
				ttlCases = append(ttlCases[:len(ttlCases)], ttlCases[len(ttlCases)+1:]...)
				//cache.mutex.Unlock()

				// fechar o canal, tirar o ttlCases
			}
		}
	}
}

/*
http://stackoverflow.com/questions/19992334/how-to-listen-to-n-channels-dynamic-select-statement
*/
