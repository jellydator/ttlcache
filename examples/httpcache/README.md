# httpcache

An example that caches HTTP server responses based on request path and query parameters. To run the
example, use the following command:

```
go run cmd/main.go
```

This will spin a HTTP server on port `:8080` with the `/reports/{name}` route. The first time you
call this endpoint with a custom name, it will take around 5 seconds to complete. All subsequent calls
to the same path within one minute will fetch you a response within some milliseconds.
