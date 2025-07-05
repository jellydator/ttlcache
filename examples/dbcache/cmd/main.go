package main

import (
	"context"
	"dbcache/internal/db"
	"dbcache/internal/db/cache"
	"dbcache/internal/order"
	"dbcache/internal/order/stream"
	"flag"
	"log/slog"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var rawExpiration string

	flag.StringVar(&rawExpiration, "exp", "0ms", "Cache expiration duration")
	flag.Parse()

	expiration, err := time.ParseDuration(rawExpiration)
	if err != nil {
		slog.With("error", err).Error("failed to parse expiration duration")
		return
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	tstamp := time.Now()

	<-runServices(ctx, expiration)

	slog.With("duration", time.Since(tstamp)).Info("shutdown complete")
}

// runServices starts the services and returns a shutdown function.
func runServices(ctx context.Context, expiration time.Duration) <-chan struct{} {
	var db order.DB = db.NewDB()

	if expiration > 0 {
		db = cache.NewCache(
			db,
			expiration,
		)
	}

	stopCh := make(chan struct{})

	go func() {
		defer close(stopCh)

		order.NewManager(
			stream.NewStreamer(),
			db,
		).Run(ctx)

		db.Close()
	}()

	return stopCh
}
