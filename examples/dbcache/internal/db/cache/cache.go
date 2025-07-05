// Package cache provides a mock implementation of a database cache.
package cache

import (
	"context"
	"dbcache/internal/order"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// Cache is an interface that defines methods for
// interacting with a database.
type Cache struct {
	db          order.DB
	volumeCache *ttlcache.Cache[string, int64]
}

// NewCache creates a new instance of the cache.
func NewCache(
	db order.DB,
	expiration time.Duration,
) *Cache {
	c := ttlcache.New(
		ttlcache.WithTTL[string, int64](expiration),
	)

	go c.Start()

	return &Cache{
		db:          db,
		volumeCache: c,
	}
}

// Close stops the cache and releases resources.
func (c *Cache) Close() error {
	c.volumeCache.Stop()
	return nil
}

// FetchAssetVolume retrieves the volume for a given asset.
func (c *Cache) FetchAssetVolume(ctx context.Context, asset string) (int64, error) {
	if item := c.volumeCache.Get(asset); item != nil {
		return item.Value(), nil
	}

	volume, err := c.db.FetchAssetVolume(ctx, asset)
	if err != nil {
		return 0, err
	}

	c.volumeCache.Set(asset, volume, ttlcache.DefaultTTL)

	return volume, nil
}

// UpsertAssetVolume updates or inserts the volume for a given asset.
func (c *Cache) UpsertAssetVolume(ctx context.Context, asset string, volume int64) error {
	err := c.db.UpsertAssetVolume(ctx, asset, volume)
	if err != nil {
		return err
	}

	c.volumeCache.Set(asset, volume, ttlcache.DefaultTTL)

	return nil
}
