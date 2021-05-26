# 2.5.0 (May 2021)

## API changes:

* #39 : Allow custom loader function for each key via `GetByLoader`

Introduce the `SimpleCache` interface for quick-start and basic usage.

# 2.4.0 (April 2021)

## API changes:

* #42 : Add option to get list of keys
* #40: Allow 'Touch' on items without other operation

// Touch resets the TTL of the key when it exists, returns ErrNotFound if the key is not present.
func (cache *Cache) Touch(key string) error 

// GetKeys returns all keys of items in the cache. Returns nil when the cache has been closed.
func (cache *Cache) GetKeys() []string 

# 2.3.0 (February 2021)

## API changes:

* #38: Added func (cache *Cache) SetExpirationReasonCallback(callback ExpireReasonCallback) This wil function will replace SetExpirationCallback(..) in the next major version.

# 2.2.0 (January 2021)

## API changes:

* #37 : a GetMetrics call is now available for some information on hits/misses etc.
*  #34 : Errors are now const

# 2.1.0 (October 2020)

## API changes

* `SetCacheSizeLimit(limit int)` a call  was contributed to set a cache limit. #35

# 2.0.0 (July 2020)

## Fixes #29, #30, #31

## Behavioural changes

* `Remove(key)` now also calls the expiration callback when it's set
* `Count()` returns zero when the cache is closed

## API changes

* `SetLoaderFunction` allows you to provide a function to retrieve data on missing cache keys.
* Operations that affect item behaviour such as `Close`, `Set`, `SetWithTTL`, `Get`, `Remove`, `Purge` now return an error with standard errors `ErrClosed` an `ErrNotFound` instead of a bool or nothing
* `SkipTTLExtensionOnHit` replaces `SkipTtlExtensionOnHit` to satisfy golint
* The callback types are now exported
