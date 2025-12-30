package webapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

// Server wraps the HTTP server configuration and lifecycle.
type Server struct {
	handler *Handler
	port    string
	server  *http.Server
}

// NewServer creates a new Server instance with all dependencies.
func NewServer(userService *user.Service, accountingRepo domain.AccountingRepo, port, botToken string, devMode bool) (*Server, error) {
	handler, err := NewHandler(userService, accountingRepo, botToken, devMode)
	if err != nil {
		return nil, fmt.Errorf("failed to create handler: %w", err)
	}

	return &Server{
		handler: handler,
		port:    port,
	}, nil
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.handler.RegisterRoutes(mux)

	s.server = &http.Server{
		Addr:         ":" + s.port,
		Handler:      chainMiddleware(recoverWrap(), logging(), rateLimit(60, time.Minute))(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("Web server started", "port", s.port)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to start server", "error", err)
		}
	}()

	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down web server...")

	if s.server == nil {
		return nil
	}

	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
		return err
	}

	return nil
}
