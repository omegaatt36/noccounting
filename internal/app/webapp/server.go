package webapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type Server struct {
	handler *Handler
	port    string
	limiter *rateLimiter
	server  *http.Server
}

func NewServer(userService *user.Service, expenseService *expense.Service, tripService *trip.Service, port, botToken string, devMode bool) (*Server, error) {
	handler, err := NewHandler(userService, expenseService, tripService, botToken, devMode)
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

	s.limiter = newRateLimiter(60, time.Minute)

	s.server = &http.Server{
		Addr:         ":" + s.port,
		Handler:      chainMiddleware(recoverWrap(), logging(), s.limiter.middleware())(mux),
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

	if s.limiter != nil {
		s.limiter.Stop()
	}

	if s.server == nil {
		return nil
	}

	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
		return err
	}

	return nil
}
