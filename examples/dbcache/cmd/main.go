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

	var dbc order.DB = db.NewDB()

	// In case expiration is not provided, we assume
	// that caching is disabled.
	if expiration > 0 {
		dbc = cache.NewCache(
			dbc,
			expiration,
		)
	}

	order.NewManager(
		stream.NewStreamer(),
		dbc,
	).Run(ctx)

	dbc.Close()

	slog.With("duration", time.Since(tstamp)).Info("shutdown complete")
}
