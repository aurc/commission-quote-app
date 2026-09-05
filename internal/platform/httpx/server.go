package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

// Health reports liveness. It is deliberately trivial and unauthenticated: it
// carries no information an attacker could use, and a health check that depends
// on a credential fails for the wrong reasons.
func Health() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

// ServeOptions configures Serve.
type ServeOptions struct {
	Port int
	// ShutdownGrace bounds how long in-flight requests may finish before the
	// process exits. It should exceed the longest request budget for the
	// component so a deploy does not fail a request that would have succeeded.
	ShutdownGrace time.Duration
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
}

// Serve runs handler until the process is signalled, then drains in-flight
// requests. It returns nil on a clean shutdown.
func Serve(ctx context.Context, log *slog.Logger, h http.Handler, opts ServeOptions) error {
	if opts.ShutdownGrace == 0 {
		opts.ShutdownGrace = 10 * time.Second
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 5 * time.Second
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 15 * time.Second
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              net.JoinHostPort("", fmt.Sprint(opts.Port)),
		Handler:           h,
		ReadHeaderTimeout: opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	// Bind before announcing. ListenAndServe would let us log "listening" and
	// then fail, which reads as a running service in the logs and sends an
	// operator looking in the wrong place.
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}
	log.InfoContext(ctx, "listening", slog.Int("port", opts.Port))

	errc := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		stop()
		log.Info("shutting down", slog.Duration("grace", opts.ShutdownGrace))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}
