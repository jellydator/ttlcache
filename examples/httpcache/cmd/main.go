package main

import (
	"context"
	"httpcache/internal/server"
	"log/slog"
	"os/signal"
	"syscall"
)

func main() {
	shutdown := runServer()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	<-ctx.Done()

	shutdown()
}

// runServer starts the HTTP server and returns a shutdown function.
func runServer() func() {
	srv := server.NewServer(":8080")

	stopCh := make(chan struct{})

	go func() {
		defer close(stopCh)

		if err := srv.Start(); err != nil {
			slog.Default().With("error", err).Error("unexpected server closure")
		}
	}()

	return func() {
		if err := srv.Stop(); err != nil {
			slog.Default().With("error", err).Error("stopping server")
		}

		<-stopCh
	}
}
