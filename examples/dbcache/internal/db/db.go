// Package db provides an interface for database operations.
package db

import (
	"context"
	"time"
)

// DB is an interface that defines methods for
// interacting with a database.
type DB struct {
	volumes map[string]int64 // Mock in-memory storage for volumes
}

// NewDB creates a new instance of the DB.
func NewDB() *DB {
	return &DB{
		volumes: make(map[string]int64),
	}
}

// Close stops the database and releases resources.
// In a real application, this would close database connections.
func (db *DB) Close() error {
	db.volumes = nil // Clear the in-memory storage
	return nil
}

// FetchAssetVolume retrieves the volume for a given asset.
// Mock implementation: In a real application, this would query the database.
func (db *DB) FetchAssetVolume(ctx context.Context, asset string) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}

	return db.volumes[asset], nil
}

// UpsertAssetVolume updates or inserts the volume for a given asset.
// Mock implementation: In a real application, this would perform an upsert operation in the database.
func (db *DB) UpsertAssetVolume(ctx context.Context, asset string, volume int64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}

	db.volumes[asset] = volume

	return nil
}
