// Package respcache provides a logic for caching HTTP responses.
package respcache

import (
	"bytes"
	"log/slog"
	"net/http"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// Cache contains required information to
// cache HTTP requests.
type Cache struct {
	log   *slog.Logger
	cache *ttlcache.Cache[string, cacheItem]
}

// NewCache creates a new Cache instance with the specified TTL.
func NewCache(ttl time.Duration) *Cache {
	c := &Cache{
		log: slog.Default().With("component", "cache"),
		cache: ttlcache.New(
			ttlcache.WithTTL[string, cacheItem](ttl),
		),
	}

	go c.cache.Start()

	return c
}

// Stop stops the automatic cleanup process.
// It blocks until the cleanup process exits.
func (c Cache) Stop() {
	c.cache.Stop()
}

// Handle is a middleware that caches the response
// based on the request path and query parameters.
func (c Cache) Handle(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RawPath + r.URL.RawQuery

		// We check if the response is already cached
		// by looking up the key in the cache.
		item := c.cache.Get(key)
		if item != nil {
			ci := item.Value()

			w.WriteHeader(ci.statusCode)
			w.Write(ci.body)

			return
		}

		// We create a custom response writer to capture the
		// response body so that we can cache it.
		rw := &responseWriter{
			w:    w,
			body: &bytes.Buffer{},

			// We set a default status code here,
			// as using Write without WriteHeader automatically
			// sets the status code to http.StatusOK.
			statusCode: http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		// After the response is written, we cache it
		// using the key we built earlier.
		c.cache.Set(
			key,
			cacheItem{
				body:       rw.body.Bytes(),
				statusCode: rw.statusCode,
			},
			ttlcache.DefaultTTL,
		)
	})
}

// cacheItem represents a cached item in the cache.
type cacheItem struct {
	body       []byte
	statusCode int
}

// responseWriter is a helper struct that is used to intercept
// http.ResponseWriter's Write method.
type responseWriter struct {
	w          http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

// Write writes the data to the connection as part of an HTTP reply.
func (wr *responseWriter) Write(buf []byte) (int, error) {
	n, err := wr.body.Write(buf)
	if err != nil {
		// unlikely to happen
		return n, err
	}

	return wr.w.Write(buf)
}

// Header returns the header map that is sent by the WriteHeader.
func (wr *responseWriter) Header() http.Header {
	return wr.w.Header()
}

// WriteHeader sends an HTTP response header with the provided status code.
func (wr *responseWriter) WriteHeader(statusCode int) {
	wr.statusCode = statusCode
	wr.w.WriteHeader(statusCode)
}
