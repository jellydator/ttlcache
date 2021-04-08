package ttlcache

import (
	"sync"
	"time"
)

// CheckExpireCallback is used as a callback for an external check on item expiration
type CheckExpireCallback func(key string, value interface{}) bool

// ExpireCallback is used as a callback on item expiration or when notifying of an item new to the cache
// Note that ExpireReasonCallback will be the succesor of this function in the next major release.
type ExpireCallback func(key string, value interface{})

// ExpireReasonCallback is used as a callback on item expiration with extra information why the item expired.
type ExpireReasonCallback func(key string, reason EvictionReason, value interface{})

// LoaderFunction can be supplied to retrieve an item where a cache miss occurs. Supply an item specific ttl or Duration.Zero
type LoaderFunction func(key string) (data interface{}, ttl time.Duration, err error)

// Cache is a synchronized map of items that can auto-expire once stale
type Cache struct {
	mutex                  sync.Mutex
	ttl                    time.Duration
	items                  map[string]*item
	loaderLock             map[string]*sync.Cond
	expireCallback         ExpireCallback
	expireReasonCallback   ExpireReasonCallback
	checkExpireCallback    CheckExpireCallback
	newItemCallback        ExpireCallback
	priorityQueue          *priorityQueue
	expirationNotification chan bool
	expirationTime         time.Time
	skipTTLExtension       bool
	shutdownSignal         chan (chan struct{})
	isShutDown             bool
	loaderFunction         LoaderFunction
	sizeLimit              int
	metrics                Metrics
}

// EvictionReason is an enum that explains why an item was evicted
type EvictionReason int

const (
	// Removed : explicitly removed from cache via API call
	Removed EvictionReason = iota
	// EvictedSize : evicted due to exceeding the cache size
	EvictedSize
	// Expired : the time to live is zero and therefore the item is removed
	Expired
	// Closed : the cache was closed
	Closed
)

const (
	// ErrClosed is raised when operating on a cache where Close() has already been called.
	ErrClosed = constError("cache already closed")
	// ErrNotFound indicates that the requested key is not present in the cache
	ErrNotFound = constError("key not found")
)

type constError string

func (err constError) Error() string {
	return string(err)
}

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
				cache.evictjob(Closed)
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

func (cache *Cache) checkExpirationCallback(item *item, reason EvictionReason) {
	if cache.expireCallback != nil {
		go cache.expireCallback(item.key, item.data)
	}
	if cache.expireReasonCallback != nil {
		go cache.expireReasonCallback(item.key, reason, item.data)
	}
}

func (cache *Cache) removeItem(item *item, reason EvictionReason) {
	cache.metrics.Evicted++
	cache.checkExpirationCallback(item, reason)
	cache.priorityQueue.remove(item)
	delete(cache.items, item.key)

}

func (cache *Cache) evictjob(reason EvictionReason) {
	// index will only be advanced if the current entry will not be evicted
	i := 0
	for item := cache.priorityQueue.items[i]; ; item = cache.priorityQueue.items[i] {

		cache.removeItem(item, reason)
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

		cache.removeItem(item, Expired)
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
		cache.Purge()
	} else {
		cache.mutex.Unlock()
		return ErrClosed
	}
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
		if cache.sizeLimit != 0 && len(cache.items) >= cache.sizeLimit {
			cache.removeItem(cache.priorityQueue.items[0], EvictedSize)
		}
		item = newItem(key, data, ttl)
		cache.items[key] = item
	}
	cache.metrics.Inserted++

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

	cache.metrics.Hits++
	item, exists, triggerExpirationNotification := cache.getItem(key)

	var dataToReturn interface{}
	if exists {
		cache.metrics.Retrievals++
		dataToReturn = item.data
	}

	var err error
	if !exists {
		cache.metrics.Misses++
		err = ErrNotFound
	}
	if cache.loaderFunction == nil || exists {
		cache.mutex.Unlock()
	}

	if cache.loaderFunction != nil && !exists {
		if lock, ok := cache.loaderLock[key]; ok {
			// if a lock is present then a fetch is in progress and we wait.
			cache.mutex.Unlock()
			lock.L.Lock()
			lock.Wait()
			lock.L.Unlock()
			cache.mutex.Lock()
			item, exists, triggerExpirationNotification = cache.getItem(key)
			if exists {
				dataToReturn = item.data
				err = nil
			}
			cache.mutex.Unlock()
		} else {
			// if no lock is present we are the leader and should set the lock and fetch.
			m := sync.NewCond(&sync.Mutex{})
			cache.loaderLock[key] = m
			cache.mutex.Unlock()
			// cache is not blocked during IO
			dataToReturn, err = cache.invokeLoader(key)
			cache.mutex.Lock()
			m.Broadcast()
			// cleanup so that we don't block consecutive access.
			delete(cache.loaderLock, key)
			cache.mutex.Unlock()
		}

	}

	if triggerExpirationNotification {
		cache.expirationNotification <- true
	}

	return dataToReturn, err
}

func (cache *Cache) invokeLoader(key string) (dataToReturn interface{}, err error) {
	var ttl time.Duration

	dataToReturn, ttl, err = cache.loaderFunction(key)
	if err == nil {
		err = cache.SetWithTTL(key, dataToReturn, ttl)
		if err != nil {
			dataToReturn = nil
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
	cache.removeItem(object, Removed)

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

// GetKeys returns all keys of items in the cache. Returns nil when the cache has been closed.
func (cache *Cache) GetKeys() []string {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	if cache.isShutDown {
		return nil
	}
	keys := make([]string, len(cache.items))
	i := 0
	for k := range cache.items {
		keys[i] = k
		i++
	}
	return keys
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

// SetExpirationReasonCallback sets a callback that will be called when an item expires, includes reason of expiry
func (cache *Cache) SetExpirationReasonCallback(callback ExpireReasonCallback) {
	cache.expireReasonCallback = callback
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
// Additional Get calls on the same key block while fetching is in progress (groupcache style).
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
	cache.metrics.Evicted += int64(len(cache.items))
	cache.items = make(map[string]*item)
	cache.priorityQueue = newPriorityQueue()
	return nil
}

// SetCacheSizeLimit sets a limit to the amount of cached items.
// If a new item is getting cached, the closes item to being timed out will be replaced
// Set to 0 to turn off
func (cache *Cache) SetCacheSizeLimit(limit int) {
	cache.sizeLimit = limit
}

// NewCache is a helper to create instance of the Cache struct
func NewCache() *Cache {

	shutdownChan := make(chan chan struct{})

	cache := &Cache{
		items:                  make(map[string]*item),
		loaderLock:             make(map[string]*sync.Cond),
		priorityQueue:          newPriorityQueue(),
		expirationNotification: make(chan bool),
		expirationTime:         time.Now(),
		shutdownSignal:         shutdownChan,
		isShutDown:             false,
		loaderFunction:         nil,
		sizeLimit:              0,
		metrics:                Metrics{},
	}
	go cache.startExpirationProcessing()
	return cache
}

// GetMetrics exposes the metrics of the cache. This is a snapshot copy of the metrics.
func (cache *Cache) GetMetrics() Metrics {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	return cache.metrics
}

// Touch resets the TTL of the key when it exists, returns ErrNotFound if the key is not present.
func (cache *Cache) Touch(key string) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	item, exists := cache.items[key]
	if !exists {
		return ErrNotFound
	}
	item.touch()
	return nil
}

func min(duration time.Duration, second time.Duration) time.Duration {
	if duration < second {
		return duration
	}
	return second
}
