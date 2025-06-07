// Package server provides functionality to create and run an HTTP server.
package server

import (
	"context"
	"fmt"
	"httpcache/internal/server/respcache"
	"log/slog"
	"net/http"
	"time"
)

// Server contains required information to
// run an HTTP server.
type Server struct {
	log   *slog.Logger
	cache *respcache.Cache
	serv  *http.Server
}

// NewServer creates a new Server instance with
// the specified address.
func NewServer(addr string) *Server {
	s := &Server{
		log:   slog.Default().With("component", "server"),
		cache: respcache.NewCache(time.Minute),
	}

	s.serv = &http.Server{
		Addr:    addr,
		Handler: s.router(),
	}

	return s
}

// Start starts the server. It blocks until the server.Stop is called.
func (s *Server) Start() error {
	s.log.With("addr", s.serv.Addr).Info("started web server")

	return s.serv.ListenAndServe()
}

// Stop shuts down the server.
func (s *Server) Stop() error {
	s.cache.Stop()

	return s.serv.Shutdown(context.Background())
}

// router sets up the HTTP routes for the server.
func (s Server) router() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /reports/{name}", s.cache.Handle(s.fetchReport))

	return mux
}

// fetchReport is the handler that fetches report information based on
// the report name provided in the URL path.
func (s Server) fetchReport(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// For the demonstration purposes, the timer below acts
	// as a placeholder for the actual report fetching logic.
	select {
	case <-time.After(5 * time.Second):
		// OK.
	case <-r.Context().Done():
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Report %q fetched successfully!", name)))
}
