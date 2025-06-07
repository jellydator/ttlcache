# httpcache

An example that caches HTTP server responses based on request path and query parameters. To run the
example, use the following command:

```
go run cmd/main.go
```

This will spin a HTTP server on port `:8080` with the `/reports/{name}` route. The first time you
call this endpoint with a custom name, it will take around 5 seconds to complete. All subsequent calls
within one minute using the same name will use a cached response and will take milliseconds to complete.

The endpoint can be called using `curl`:

```
curl 127.0.0.1:8080/reports/hello
```
