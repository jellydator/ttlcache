## TTLCache - an in-memory LRU cache with expiration

TTLCache is a wrapper over a map[string]interface{} in golang, entries of which are

1. Thread-safe
2. Individual expiring time or global expiring time, you can choose
3. Auto-Extending expiration on `Get`s

[![Build Status](https://travis-ci.org/wunderlist/ttlcache.svg)](https://travis-ci.org/wunderlist/ttlcache)

#### Usage
```go
import (
  "time"
  "github.com/wunderlist/ttlcache"
  "fmt"
)

func main () {
  evictionFunc := func(key string, value interface{}) {
		fmt.Printf("This key(%s) has expired\n", key)
	}

  // This duration is the default expiration in case of using `Set`
  cache := ttlcache.NewCache(time.Second)
  cache.SetEvictionFunction(evictionFunc)

  cache.Set("key", "value")
  cache.SetWithTTL("keyWithTTL", "value", 10 * time.Second)

  value, exists := cache.Get("key")
  count := cache.Count()
}
```

#### Original Project

TTLCache was forked from [![wunderlist/ttlcache])](https://github.com/wunderlist/ttlcache) to add extra functions not avaiable in the original scope.

The differences are

1. A item can store any kind of object, previously, only strings could be saved
2. There is a option to add a callback to get cache evictions
3. The expiration can be either global or individual per item
