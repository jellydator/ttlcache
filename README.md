# TTLCache - an in-memory cache with item expiration and generics

[![Go Reference](https://pkg.go.dev/badge/github.com/jellydator/ttlcache/v3.svg)](https://pkg.go.dev/github.com/jellydator/ttlcache/v3)
[![Build Status](https://github.com/jellydator/ttlcache/actions/workflows/go.yml/badge.svg)](https://github.com/jellydator/ttlcache/actions/workflows/go.yml)
[![Coverage Status](https://coveralls.io/repos/github/jellydator/ttlcache/badge.svg?branch=v3)](https://coveralls.io/github/jellydator/ttlcache?branch=v3)

## Features
- Simple API built with type parameters (generics)
- Per-item or cache-wide TTL with automatic deletion of expired items
- Automatic expiration time extension on each `Get` call (can be disabled)
- `Loader` interface that may be used to load/lazily initialize missing
  cache items, with optional duplicate call suppression
- Capacity limits based on the number of items or their custom-calculated cost
- Event handlers (insertion, update, and eviction)
- Metrics
- Thread safety

## Installation
```
go get github.com/jellydator/ttlcache/v3
```

## Usage
All cache operations are provided by the `Cache` type, which represents
a single in-memory data store. To create a new instance of it, the
`ttlcache.New()` function needs to be called:
```go
func main() {
	cache := ttlcache.New[string, string]()
}
```

By default, items never expire and are never removed automatically.
Expiration is enabled by setting a default TTL with the `ttlcache.WithTTL()`
option and starting the automatic cleanup process with the `cache.Start()`
method. Since `cache.Start()` blocks until `cache.Stop()` is called, it is
usually launched on a separate goroutine:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	go cache.Start() // starts automatic expired item deletion
	defer cache.Stop()
}
```

Automatic cleanup suits most applications, but some may need to control
the exact timing of expired item deletion. For example, a system may
want to delete such items only when its resource load is at its lowest
(e.g., after midnight, when the number of users/HTTP requests drops).
In cases like these, the `cache.DeleteExpired()` method can be called
periodically instead of starting the cleanup process:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	for {
		time.Sleep(4 * time.Hour)
		cache.DeleteExpired()
	}
}
```

The data stored in the cache can be inserted, retrieved, checked, and
deleted with `Set`, `Get`, `Has`, `Delete`, and other related methods.
Each new item receives a TTL: a specific duration, `ttlcache.DefaultTTL`
to use the cache's default one, or `ttlcache.NoTTL` to never expire:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	// insert data
	cache.Set("first", "value1", ttlcache.DefaultTTL)
	cache.Set("second", "value2", ttlcache.NoTTL)
	cache.Set("third", "value3", time.Minute)

	// retrieve data
	item := cache.Get("first")
	fmt.Println(item.Value(), item.ExpiresAt())

	// check whether data exists
	ok := cache.Has("third")

	// delete data
	cache.Delete("second")
	cache.DeleteExpired()
	cache.DeleteAll()

	// retrieve data if it exists, insert it otherwise
	item, found := cache.GetOrSet("fourth", "value4", ttlcache.WithTTL[string, string](time.Minute))

	// retrieve and delete data
	item, present := cache.GetAndDelete("fourth")
}
```

The `cache.OnInsertion()`, `cache.OnUpdate()`, and `cache.OnEviction()`
methods subscribe to the cache's events. The subscribed functions are
executed on separate goroutines, so they never block the cache's
operations, and each subscription method returns a function that can
be called to unsubscribe:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
		ttlcache.WithCapacity[string, string](300),
	)

	cache.OnInsertion(func(ctx context.Context, item *ttlcache.Item[string, string]) {
		fmt.Println(item.Value(), item.ExpiresAt())
	})
	cache.OnUpdate(func(ctx context.Context, item *ttlcache.Item[string, string]) {
		fmt.Println(item.Value(), item.ExpiresAt())
	})
	unsubscribe := cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, string]) {
		if reason == ttlcache.EvictionReasonCapacityReached {
			fmt.Println(item.Key(), item.Value())
		}
	})

	cache.Set("first", "value1", ttlcache.DefaultTTL)
	cache.DeleteAll()

	// stop receiving eviction events
	unsubscribe()
}
```

A custom or existing implementation of the `ttlcache.Loader` interface
can be used to load or lazily initialize data on cache misses. The
`Get` method calls the loader whenever the requested item is not found
and returns whatever the loader returns:
```go
func main() {
	loader := ttlcache.LoaderFunc[string, string](
		func(c *ttlcache.Cache[string, string], key string) *ttlcache.Item[string, string] {
			// load from file/make an HTTP request
			item := c.Set(key, "value from file", ttlcache.DefaultTTL)
			return item
		},
	)
	cache := ttlcache.New[string, string](
		ttlcache.WithLoader[string, string](loader),
	)

	item := cache.Get("key from file")
}
```

When multiple goroutines request the same missing item at once, the
loader normally runs once for each of them. Wrapping it with
`ttlcache.NewSuppressedLoader()` ensures that only one load operation
is in-flight for a given key at a time, with all callers receiving
its result:
```go
func main() {
	loader := ttlcache.LoaderFunc[string, string](
		func(c *ttlcache.Cache[string, string], key string) *ttlcache.Item[string, string] {
			// load from file/make an HTTP request
			item := c.Set(key, "value from file", ttlcache.DefaultTTL)
			return item
		},
	)
	cache := ttlcache.New[string, string](
		ttlcache.WithLoader[string, string](ttlcache.NewSuppressedLoader(loader, nil)),
	)

	item := cache.Get("key from file")
}
```

The cache's capacity can also be restricted by criteria other than the
number of items. The `ttlcache.WithMaxCost()` option assigns each item
a cost, calculated by a custom function, and evicts the least recently
used items whenever the total cost exceeds the given limit. The
following example limits the memory used by cached entries to ~5KiB:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithMaxCost[string, string](5120, func(item ttlcache.CostItem[string, string]) uint64 {
			// Note: the calculation below does not include the memory
			// used by the internal structures or the string metadata of
			// the key and the value.
			return uint64(len(item.Key) + len(item.Value))
		}),
	)

	cache.Set("first", "value1", ttlcache.DefaultTTL)
}
```

## Examples
See the [examples](https://github.com/jellydator/ttlcache/tree/v3/examples)
directory for complete applications demonstrating how to use `ttlcache`.

## License
[MIT](LICENSE)
