package ttlcache

import (
	"sync"
	"time"
)

// CheckExpireCallback is used as a callback for an external check on item expiration
type checkExpireCallback func(key string, value interface{}) bool

// ExpireCallback is used as a callback on item expiration or when notifying of an item new to the cache
type expireCallback func(key string, value interface{})

// Cache is a synchronized map of items that can auto-expire once stale
type Cache struct {
	mutex                  sync.Mutex
	ttl                    time.Duration
	items                  map[string]*item
	expireCallback         expireCallback
	checkExpireCallback    checkExpireCallback
	newItemCallback        expireCallback
	priorityQueue          *priorityQueue
	expirationNotification chan bool
	expirationTime         time.Time
}

func (cache *Cache) getItem(key string) (*item, bool) {
	cache.mutex.Lock()

	item, exists := cache.items[key]
	if !exists || item.expired() {
		cache.mutex.Unlock()
		return nil, false
	}

	if item.ttl >= 0 && (item.ttl > 0 || cache.ttl > 0) {
		if cache.ttl > 0 && item.ttl == 0 {
			item.ttl = cache.ttl
		}

		item.touch()
		cache.priorityQueue.update(item)
	}

	cache.mutex.Unlock()
	cache.expirationNotificationTrigger(item)
	return item, exists
}

func (cache *Cache) startExpirationProcessing() {
	for {
		var sleepTime time.Duration
		cache.mutex.Lock()
		if cache.priorityQueue.Len() > 0 {
			sleepTime = cache.priorityQueue.items[0].expireAt.Sub(time.Now())
			if sleepTime < 0 && cache.priorityQueue.items[0].expireAt.IsZero(){
				sleepTime = time.Hour
			} else if sleepTime < 0 {
				sleepTime = time.Microsecond
			}
			if cache.ttl > 0 {
				sleepTime = min(sleepTime, cache.ttl)
			}

		} else if cache.ttl > 0 {
			sleepTime = cache.ttl
		} else {
			sleepTime = time.Hour
		}

		cache.expirationTime = time.Now().Add(sleepTime)
		cache.mutex.Unlock()

		select {
		case <-time.After(sleepTime):
			cache.mutex.Lock()
			if cache.priorityQueue.Len() == 0 {
				cache.mutex.Unlock()
				continue
			}

			// index will only be advanced if the current entry will not be evicted
			i := 0
			for item := cache.priorityQueue.items[i]; item.expired(); item = cache.priorityQueue.items[i] {

				if cache.checkExpireCallback != nil {
					if !cache.checkExpireCallback(item.key, item.data) {
						item.touch()
						cache.priorityQueue.update(item)
						i++
						continue
					}
				}

				cache.priorityQueue.remove(item)
				delete(cache.items, item.key)
				if cache.expireCallback != nil {
					go cache.expireCallback(item.key, item.data)
				}
				if cache.priorityQueue.Len() == 0 {
					goto done
				}
			}
		done:
			cache.mutex.Unlock()

		case <-cache.expirationNotification:
			continue
		}
	}
}

func (cache *Cache) expirationNotificationTrigger(item *item) {
	cache.mutex.Lock()
	if cache.expirationTime.After(time.Now().Add(item.ttl)) {
		cache.mutex.Unlock()
		cache.expirationNotification <- true
	} else {
		cache.mutex.Unlock()
	}
}

// Set is a thread-safe way to add new items to the map
func (cache *Cache) Set(key string, data interface{}) {
	cache.SetWithTTL(key, data, ItemExpireWithGlobalTTL)
}

// SetWithTTL is a thread-safe way to add new items to the map with individual ttl
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) {
	item, exists := cache.getItem(key)
	cache.mutex.Lock()

	if exists {
		item.data = data
		item.ttl = ttl
	} else {
		item = newItem(key, data, ttl)
		cache.items[key] = item
	}

	if item.ttl >= 0 && (item.ttl > 0 || cache.ttl > 0) {
		if cache.ttl > 0 && item.ttl == 0 {
			item.ttl = cache.ttl
		}
		item.touch()
	}

	if exists {
		cache.priorityQueue.update(item)
	} else {
		cache.priorityQueue.push(item)
	}

	cache.mutex.Unlock()
	if !exists && cache.newItemCallback != nil {
		cache.newItemCallback(key, data)
	}
	cache.expirationNotification <- true
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (interface{}, bool) {
	item, exists := cache.getItem(key)
	if exists {
		return item.data, true
	}
	return nil, false
}

func (cache *Cache) Remove(key string) bool {
	cache.mutex.Lock()
	object, exists := cache.items[key]
	if !exists {
		cache.mutex.Unlock()
		return false
	}
	delete(cache.items, object.key)
	cache.priorityQueue.remove(object)
	cache.mutex.Unlock()

	return true
}

// Count returns the number of items in the cache
func (cache *Cache) Count() int {
	cache.mutex.Lock()
	length := len(cache.items)
	cache.mutex.Unlock()
	return length
}

func (cache *Cache) SetTTL(ttl time.Duration) {
	cache.mutex.Lock()
	cache.ttl = ttl
	cache.mutex.Unlock()
	cache.expirationNotification <- true
}

// SetExpirationCallback sets a callback that will be called when an item expires
func (cache *Cache) SetExpirationCallback(callback expireCallback) {
	cache.expireCallback = callback
}

// SetCheckExpirationCallback sets a callback that will be called when an item is about to expire
// in order to allow external code to decide whether the item expires or remains for another TTL cycle
func (cache *Cache) SetCheckExpirationCallback(callback checkExpireCallback) {
	cache.checkExpireCallback = callback
}

// SetNewItemCallback sets a callback that will be called when a new item is added to the cache
func (cache *Cache) SetNewItemCallback(callback expireCallback) {
	cache.newItemCallback = callback
}

// Purge will remove all entries
func (cache *Cache) Purge() {
	cache.mutex.Lock()
	cache.items = make(map[string]*item)
	cache.priorityQueue = newPriorityQueue()
	cache.mutex.Unlock()
}

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {
	cache := &Cache{
		items:                  make(map[string]*item),
		priorityQueue:          newPriorityQueue(),
		expirationNotification: make(chan bool),
		expirationTime:         time.Now(),
	}
	go cache.startExpirationProcessing()
	return cache
}

func min(duration time.Duration, second time.Duration) time.Duration {
	if duration < second {
		return duration
	}
	return second
}
