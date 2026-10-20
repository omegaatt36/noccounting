package webapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/omegaatt36/noccounting/internal/service/expense"
	"github.com/omegaatt36/noccounting/internal/service/trip"
	"github.com/omegaatt36/noccounting/internal/service/user"
)

type Server struct {
	handler *Handler
	port    string
	// host is the address the server listens on; empty is every interface.
	host     string
	listener net.Listener
	limiter  *rateLimiter
	server   *http.Server
}

func NewServer(userService *user.Service, expenseService *expense.Service, tripService *trip.Service, port, botToken string, devMode bool) (*Server, error) {
	handler, err := NewHandler(userService, expenseService, tripService, botToken, devMode)
	if err != nil {
		return nil, fmt.Errorf("failed to create handler: %w", err)
	}

	// Dev mode answers anyone as the first mapped user, so it never listens beyond loopback.
	host := ""
	if devMode {
		host = "127.0.0.1"
	}

	return &Server{
		handler: handler,
		port:    port,
		host:    host,
		limiter: newRateLimiter(60, time.Minute),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.handler.RegisterRoutes(mux)
	return chainMiddleware(recoverWrap(), logging(), securityHeaders(), s.limiter.middleware())(mux)
}

const (
	readTimeout = 15 * time.Second
	// writeTimeout must outlast the slowest handler without a deadline of its own: those wait up to 30 seconds on TREK.
	writeTimeout = 45 * time.Second
)

// Start fails on a port it cannot bind rather than logging, so a dead Mini App does not leave a bot-only process running.
func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:         net.JoinHostPort(s.host, s.port),
		Handler:      s.Handler(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.server.Addr, err)
	}

	s.listener = listener

	go func() {
		slog.Info("Web server started", "addr", listener.Addr().String())
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Web server stopped", "error", err)
		}
	}()

	return nil
}

func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down web server...")

	s.limiter.Stop()

	if s.server == nil {
		return nil
	}

	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
		return err
	}

	return nil
}
