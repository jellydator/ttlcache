package ttlcache

import (
	"errors"
	"sync"
	"time"
)

// CheckExpireCallback is used as a callback for an external check on item expiration
type CheckExpireCallback func(key string, value interface{}) bool

// ExpireCallback is used as a callback on item expiration or when notifying of an item new to the cache
type ExpireCallback func(key string, value interface{})

// LoaderFunction can be supplied to retrieve an item where a cache miss occurs. Supply an item specific ttl or Duration.Zero
type LoaderFunction func(key string) (data interface{}, ttl time.Duration, err error)

// Cache is a synchronized map of items that can auto-expire once stale
type Cache struct {
	mutex                  sync.Mutex
	ttl                    time.Duration
	items                  map[string]*item
	expireCallback         ExpireCallback
	checkExpireCallback    CheckExpireCallback
	newItemCallback        ExpireCallback
	priorityQueue          *priorityQueue
	expirationNotification chan bool
	expirationTime         time.Time
	skipTTLExtension       bool
	shutdownSignal         chan (chan struct{})
	isShutDown             bool
	loaderFunction         LoaderFunction
}

var (
	// ErrClosed is raised when operating on a cache where Close() has already been called.
	ErrClosed = errors.New("cache already closed")
	// ErrNotFound indicates that the requested key is not present in the cache
	ErrNotFound = errors.New("key not found")
)

func (cache *Cache) getItem(key string) (*item, bool, bool) {
	item, exists := cache.items[key]
	if !exists || item.expired() {
		return nil, false, false
	}

	if item.ttl >= 0 && (item.ttl > 0 || cache.ttl > 0) {
		if cache.ttl > 0 && item.ttl == 0 {
			item.ttl = cache.ttl
		}

		if !cache.skipTTLExtension {
			item.touch()
		}
		cache.priorityQueue.update(item)
	}

	expirationNotification := false
	if cache.expirationTime.After(time.Now().Add(item.ttl)) {
		expirationNotification = true
	}
	return item, exists, expirationNotification
}

func (cache *Cache) startExpirationProcessing() {
	timer := time.NewTimer(time.Hour)
	for {
		var sleepTime time.Duration
		cache.mutex.Lock()
		if cache.priorityQueue.Len() > 0 {
			sleepTime = time.Until(cache.priorityQueue.items[0].expireAt)
			if sleepTime < 0 && cache.priorityQueue.items[0].expireAt.IsZero() {
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

		timer.Reset(sleepTime)
		select {
		case shutdownFeedback := <-cache.shutdownSignal:
			timer.Stop()
			cache.mutex.Lock()
			if cache.priorityQueue.Len() > 0 {
				cache.evictjob()
			}
			cache.mutex.Unlock()
			shutdownFeedback <- struct{}{}
			return
		case <-timer.C:
			timer.Stop()
			cache.mutex.Lock()
			if cache.priorityQueue.Len() == 0 {
				cache.mutex.Unlock()
				continue
			}

			cache.cleanjob()
			cache.mutex.Unlock()

		case <-cache.expirationNotification:
			timer.Stop()
			continue
		}
	}
}

func (cache *Cache) removeItem(item *item) {
	if cache.expireCallback != nil {
		go cache.expireCallback(item.key, item.data)
	}
	cache.priorityQueue.remove(item)
	delete(cache.items, item.key)

}

func (cache *Cache) evictjob() {
	// index will only be advanced if the current entry will not be evicted
	i := 0
	for item := cache.priorityQueue.items[i]; ; item = cache.priorityQueue.items[i] {

		cache.removeItem(item)
		if cache.priorityQueue.Len() == 0 {
			return
		}
	}
}

func (cache *Cache) cleanjob() {
	// index will only be advanced if the current entry will not be evicted
	i := 0
	for item := cache.priorityQueue.items[i]; item.expired(); item = cache.priorityQueue.items[i] {

		if cache.checkExpireCallback != nil {
			if !cache.checkExpireCallback(item.key, item.data) {
				item.touch()
				cache.priorityQueue.update(item)
				i++
				if i == cache.priorityQueue.Len() {
					break
				}
				continue
			}
		}

		cache.removeItem(item)
		if cache.priorityQueue.Len() == 0 {
			return
		}
	}
}

// Close calls Purge after stopping the goroutine that does ttl checking, for a clean shutdown.
// The cache is no longer cleaning up after the first call to Close, repeated calls are safe and return ErrClosed.
func (cache *Cache) Close() error {

	cache.mutex.Lock()
	if !cache.isShutDown {
		cache.isShutDown = true
		cache.mutex.Unlock()
		feedback := make(chan struct{})
		cache.shutdownSignal <- feedback
		<-feedback
		close(cache.shutdownSignal)
	} else {
		cache.mutex.Unlock()
		return ErrClosed
	}
	cache.Purge()
	return nil
}

// Set is a thread-safe way to add new items to the map.
func (cache *Cache) Set(key string, data interface{}) error {
	return cache.SetWithTTL(key, data, ItemExpireWithGlobalTTL)
}

// SetWithTTL is a thread-safe way to add new items to the map with individual ttl.
func (cache *Cache) SetWithTTL(key string, data interface{}, ttl time.Duration) error {
	cache.mutex.Lock()
	if cache.isShutDown {
		cache.mutex.Unlock()
		return ErrClosed
	}
	item, exists, _ := cache.getItem(key)

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
	return nil
}

// Get is a thread-safe way to lookup items
// Every lookup, also touches the item, hence extending it's life
func (cache *Cache) Get(key string) (interface{}, error) {
	cache.mutex.Lock()
	if cache.isShutDown {
		cache.mutex.Unlock()
		return nil, ErrClosed
	}
	item, exists, triggerExpirationNotification := cache.getItem(key)

	var dataToReturn interface{}
	if exists {
		dataToReturn = item.data
	}
	cache.mutex.Unlock()
	if triggerExpirationNotification {
		cache.expirationNotification <- true
	}
	var err error = nil
	if !exists {
		err = ErrNotFound

		if cache.loaderFunction != nil {
			var ttl time.Duration
			dataToReturn, ttl, err = cache.loaderFunction(key)
			if err == nil {
				err = cache.SetWithTTL(key, dataToReturn, ttl)
				if err != nil {
					dataToReturn = nil
				}
			}
		}

	}
	return dataToReturn, err
}

// Remove removes an item from the cache if it exists, triggers expiration callback when set. Can return ErrNotFound if the entry was not present.
func (cache *Cache) Remove(key string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.isShutDown {
		return ErrClosed
	}

	object, exists := cache.items[key]
	if !exists {
		return ErrNotFound
	}
	cache.removeItem(object)

	return nil
}

// Count returns the number of items in the cache. Returns zero when the cache has been closed.
func (cache *Cache) Count() int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if cache.isShutDown {
		return 0
	}
	length := len(cache.items)
	return length
}

// SetTTL sets the global TTL value for items in the cache, which can be overridden at the item level.
func (cache *Cache) SetTTL(ttl time.Duration) error {
	cache.mutex.Lock()

	if cache.isShutDown {
		cache.mutex.Unlock()
		return ErrClosed
	}
	cache.ttl = ttl
	cache.mutex.Unlock()
	cache.expirationNotification <- true
	return nil
}

// SetExpirationCallback sets a callback that will be called when an item expires
func (cache *Cache) SetExpirationCallback(callback ExpireCallback) {
	cache.expireCallback = callback
}

// SetCheckExpirationCallback sets a callback that will be called when an item is about to expire
// in order to allow external code to decide whether the item expires or remains for another TTL cycle
func (cache *Cache) SetCheckExpirationCallback(callback CheckExpireCallback) {
	cache.checkExpireCallback = callback
}

// SetNewItemCallback sets a callback that will be called when a new item is added to the cache
func (cache *Cache) SetNewItemCallback(callback ExpireCallback) {
	cache.newItemCallback = callback
}

// SkipTTLExtensionOnHit allows the user to change the cache behaviour. When this flag is set to true it will
// no longer extend TTL of items when they are retrieved using Get, or when their expiration condition is evaluated
// using SetCheckExpirationCallback.
func (cache *Cache) SkipTTLExtensionOnHit(value bool) {
	cache.skipTTLExtension = value
}

// SetLoaderFunction allows you to set a function to retrieve cache misses. The signature matches that of the Get function.
func (cache *Cache) SetLoaderFunction(loader LoaderFunction) {
	cache.loaderFunction = loader
}

// Purge will remove all entries
func (cache *Cache) Purge() error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.isShutDown {
		return ErrClosed
	}
	cache.items = make(map[string]*item)
	cache.priorityQueue = newPriorityQueue()
	return nil
}

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {

	shutdownChan := make(chan chan struct{})

	cache := &Cache{
		items:                  make(map[string]*item),
		priorityQueue:          newPriorityQueue(),
		expirationNotification: make(chan bool),
		expirationTime:         time.Now(),
		shutdownSignal:         shutdownChan,
		isShutDown:             false,
		loaderFunction:         nil,
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
