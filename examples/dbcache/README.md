# dbcache

An example that implements a cache for the database layer. It caches recent reads and writes, so all subsequent reads are faster. To run the
example, use the following command:

```
go run cmd/main.go
```

This will run a mock service that consumes orders from the streamer package. Once the processing is done, the application exits and outputs the
time it took to process them. To preview the capabilities of the cache, run the example several times using different caching times.

```
go run cmd/main.go -exp=200ms
```

```
go run cmd/main.go -exp=500ms
```
