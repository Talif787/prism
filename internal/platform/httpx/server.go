package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ServerConfig captures the tunable timeouts for the HTTP server.
type ServerConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// Server owns an http.Server and its graceful lifecycle.
type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func NewServer(cfg ServerConfig, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadTimeout:       cfg.ReadTimeout,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       60 * time.Second,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger,
	}
}

// Run starts serving and blocks until ctx is cancelled, then drains connections
// within the shutdown timeout so in-flight requests complete cleanly.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", slog.String("addr", s.srv.Addr))
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, s.srv.Close())
		}
		return nil
	}
}
