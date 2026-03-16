package server

import (
	"context"
	"net/http"
	"time"

	"github.com/nlook-service/nlook-router/internal/tools"
)

// Server is the local HTTP API (health, status, tools).
type Server struct {
	addr        string
	mux         *http.ServeMux
	httpServer  *http.Server
	status      *Status
	toolsLister tools.Lister
}

// Status holds runtime status for GET /status.
type Status struct {
	RouterID  string `json:"router_id"`
	Connected bool   `json:"connected"`
}

// New creates a new server.
func New(addr string, status *Status) *Server {
	s := &Server{
		addr:   addr,
		mux:    http.NewServeMux(),
		status: status,
	}
	s.setupRoutes()
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return s
}

// SetToolsLister sets the lister for GET /tools. If not set, /tools returns 503.
func (s *Server) SetToolsLister(l tools.Lister) {
	s.toolsLister = l
}

// ListenAndServe blocks until the server is stopped.
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the listen address (e.g. for logging).
func (s *Server) Addr() string {
	return s.addr
}
