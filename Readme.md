## TTLCache - an in-memory LRU cache with expiration

TTLCache is a simple key/value cache in golang with the following functions:

1. Thread-safe
2. Individual expiring time or global expiring time, you can choose
3. Auto-Extending expiration on `Get`s
4. Fast and memory efficient

[![Build Status](https://travis-ci.org/diegobernardes/ttlcache.svg)](https://travis-ci.org/diegobernardes/ttlcache)

#### Usage
```go
import (
  "time"
  "github.com/diegobernardes/ttlcache"
  "fmt"
)

func main () {
  expirationCallback := func(key string, value interface{}) {
		fmt.Printf("This key(%s) has expired\n", key)
	}

  cache := ttlcache.NewCache()
  cache.SetExpireCallback(expirationCallback)

  cache.Set("key", "value")
  cache.SetWithTTL("keyWithTTL", "value", 10 * time.Second)

  value, exists := cache.Get("key")
  count := cache.Count()
}
```

#### Original Project

TTLCache was forked from [wunderlist/ttlcache](https://github.com/wunderlist/ttlcache) to add extra functions not avaiable in the original scope

The main differences are:

1. A item can store any kind of object, previously, only strings could be saved
2. There is a option to add a callback to get key expiration
3. The expiration can be either global or individual per item
4. Can exist items without expiration time
