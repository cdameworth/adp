// Package server provides the HTTP server with graceful shutdown for ADP.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Server wraps http.Server with graceful shutdown capabilities
type Server struct {
	httpServer       *http.Server
	config           Config
	logger           *slog.Logger
	shutdownHooks    []ShutdownHook
	healthCheck      HealthChecker
	isShuttingDown   atomic.Bool
	connectionsWg    sync.WaitGroup
	shutdownComplete chan struct{}
}

// Config holds server configuration
type Config struct {
	Addr            string
	Handler         http.Handler
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxHeaderBytes  int
}

// ShutdownHook is called during graceful shutdown
type ShutdownHook func(ctx context.Context) error

// HealthChecker provides health status for the server
type HealthChecker interface {
	SetReady(ready bool)
	IsReady() bool
}

// DefaultConfig returns default server configuration
func DefaultConfig() Config {
	return Config{
		Addr:            ":8080",
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		MaxHeaderBytes:  1 << 20, // 1 MB
	}
}

// New creates a new server
func New(cfg Config, logger *slog.Logger) *Server {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	if logger == nil {
		logger = slog.Default()
	}

	srv := &Server{
		config:           cfg,
		logger:           logger,
		shutdownHooks:    []ShutdownHook{},
		shutdownComplete: make(chan struct{}),
	}

	srv.httpServer = &http.Server{
		Addr:           cfg.Addr,
		Handler:        srv.wrapHandler(cfg.Handler),
		ReadTimeout:    cfg.ReadTimeout,
		WriteTimeout:   cfg.WriteTimeout,
		IdleTimeout:    cfg.IdleTimeout,
		MaxHeaderBytes: cfg.MaxHeaderBytes,
	}

	return srv
}

// wrapHandler adds connection tracking middleware
func (s *Server) wrapHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if shutting down
		if s.isShuttingDown.Load() {
			w.Header().Set("Connection", "close")
			http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
			return
		}

		// Track connection
		s.connectionsWg.Add(1)
		defer s.connectionsWg.Done()

		handler.ServeHTTP(w, r)
	})
}

// SetHealthChecker sets the health checker for readiness probes
func (s *Server) SetHealthChecker(hc HealthChecker) {
	s.healthCheck = hc
}

// AddShutdownHook adds a hook to be called during graceful shutdown
func (s *Server) AddShutdownHook(hook ShutdownHook) {
	s.shutdownHooks = append(s.shutdownHooks, hook)
}

// ListenAndServe starts the server and handles graceful shutdown
func (s *Server) ListenAndServe() error {
	return s.listenAndServe(false, "", "")
}

// ListenAndServeTLS starts the server with TLS and handles graceful shutdown
func (s *Server) ListenAndServeTLS(certFile, keyFile string) error {
	return s.listenAndServe(true, certFile, keyFile)
}

func (s *Server) listenAndServe(tls bool, certFile, keyFile string) error {
	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		s.logger.Info("Starting server", "addr", s.config.Addr)

		// Mark as ready for health checks
		if s.healthCheck != nil {
			s.healthCheck.SetReady(true)
		}

		var err error
		if tls {
			err = s.httpServer.ListenAndServeTLS(certFile, keyFile)
		} else {
			err = s.httpServer.ListenAndServe()
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
		close(errChan)
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return fmt.Errorf("server failed: %w", err)
	case sig := <-sigChan:
		s.logger.Info("Received shutdown signal", "signal", sig)
		return s.shutdown()
	}
}

// shutdown performs graceful shutdown
func (s *Server) shutdown() error {
	// Mark as shutting down
	s.isShuttingDown.Store(true)

	// Mark as not ready for health checks (stop receiving new traffic)
	if s.healthCheck != nil {
		s.healthCheck.SetReady(false)
	}

	s.logger.Info("Starting graceful shutdown", "timeout", s.config.ShutdownTimeout)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	// Create channel for shutdown errors
	errChan := make(chan error, 1)

	go func() {
		// Phase 1: Stop accepting new connections
		s.logger.Info("Phase 1: Stopping new connections")
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.Error("Error during HTTP shutdown", "error", err)
		}

		// Phase 2: Wait for active connections to complete
		s.logger.Info("Phase 2: Waiting for active connections to complete")
		done := make(chan struct{})
		go func() {
			s.connectionsWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			s.logger.Info("All connections completed")
		case <-ctx.Done():
			s.logger.Warn("Timeout waiting for connections, forcing close")
		}

		// Phase 3: Run shutdown hooks
		s.logger.Info("Phase 3: Running shutdown hooks", "count", len(s.shutdownHooks))
		var shutdownErrors []error
		for i, hook := range s.shutdownHooks {
			if err := hook(ctx); err != nil {
				s.logger.Error("Shutdown hook failed", "index", i, "error", err)
				shutdownErrors = append(shutdownErrors, err)
			}
		}

		if len(shutdownErrors) > 0 {
			errChan <- fmt.Errorf("shutdown hooks failed: %d errors", len(shutdownErrors))
		} else {
			errChan <- nil
		}
	}()

	// Wait for shutdown to complete or timeout
	select {
	case err := <-errChan:
		close(s.shutdownComplete)
		s.logger.Info("Graceful shutdown complete")
		return err
	case <-ctx.Done():
		close(s.shutdownComplete)
		s.logger.Error("Graceful shutdown timeout exceeded")
		return ctx.Err()
	}
}

// Shutdown initiates graceful shutdown (for programmatic shutdown)
func (s *Server) Shutdown(ctx context.Context) error {
	s.isShuttingDown.Store(true)

	if s.healthCheck != nil {
		s.healthCheck.SetReady(false)
	}

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP shutdown failed: %w", err)
	}

	// Wait for active connections
	done := make(chan struct{})
	go func() {
		s.connectionsWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Run shutdown hooks
	for _, hook := range s.shutdownHooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}

	return nil
}

// WaitForShutdown blocks until shutdown is complete
func (s *Server) WaitForShutdown() {
	<-s.shutdownComplete
}

// IsShuttingDown returns whether the server is in shutdown mode
func (s *Server) IsShuttingDown() bool {
	return s.isShuttingDown.Load()
}

// SimpleHealthChecker is a basic health checker implementation
type SimpleHealthChecker struct {
	ready atomic.Bool
}

// NewSimpleHealthChecker creates a new simple health checker
func NewSimpleHealthChecker() *SimpleHealthChecker {
	return &SimpleHealthChecker{}
}

func (h *SimpleHealthChecker) SetReady(ready bool) {
	h.ready.Store(ready)
}

func (h *SimpleHealthChecker) IsReady() bool {
	return h.ready.Load()
}

// DrainMiddleware returns middleware that rejects requests during shutdown
func DrainMiddleware(srv *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if srv.IsShuttingDown() {
				w.Header().Set("Connection", "close")
				w.Header().Set("Retry-After", "30")
				http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ConnectionCounter tracks active connections
type ConnectionCounter struct {
	count atomic.Int64
}

// NewConnectionCounter creates a new connection counter
func NewConnectionCounter() *ConnectionCounter {
	return &ConnectionCounter{}
}

// Increment increments the connection count
func (c *ConnectionCounter) Increment() {
	c.count.Add(1)
}

// Decrement decrements the connection count
func (c *ConnectionCounter) Decrement() {
	c.count.Add(-1)
}

// Count returns the current connection count
func (c *ConnectionCounter) Count() int64 {
	return c.count.Load()
}

// ConnectionCounterMiddleware tracks active connections
func ConnectionCounterMiddleware(counter *ConnectionCounter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			counter.Increment()
			defer counter.Decrement()
			next.ServeHTTP(w, r)
		})
	}
}
