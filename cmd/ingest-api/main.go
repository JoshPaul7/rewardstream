// Command ingest-api accepts ad events over HTTP and publishes them to SNS.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/JoshPaul7/rewardstream/internal/awsenv"
	"github.com/JoshPaul7/rewardstream/internal/ingest"
	"github.com/JoshPaul7/rewardstream/internal/snspub"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("ingest-api stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	mode := awsenv.Mode(os.Getenv("RS_MODE"))
	topicARN := os.Getenv("RS_TOPIC_ARN")
	if topicARN == "" {
		return errors.New("RS_TOPIC_ARN is required")
	}
	addr := envOr("RS_HTTP_ADDR", ":8080")
	drain, err := time.ParseDuration(envOr("RS_SHUTDOWN_DRAIN", "5s"))
	if err != nil {
		return fmt.Errorf("RS_SHUTDOWN_DRAIN: %w", err)
	}

	// Root context: cancelled on Ctrl-C or SIGTERM (Kubernetes, docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	cfg, err := awsenv.Load(startCtx, mode, os.Getenv("RS_EXPECTED_ACCOUNT_ID"))
	cancel()
	if err != nil {
		return err
	}

	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("POST /v1/events", &ingest.Handler{Pub: snspub.New(cfg, topicARN), Log: log, Now: time.Now})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "mode", mode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	ready.Store(true)

	select {
	case err := <-errCh:
		return err // e.g. port already in use
	case <-ctx.Done():
	}

	log.Info("shutdown: marking not ready", "drain", drain.String())
	ready.Store(false)
	time.Sleep(drain) // give Endpoints / load balancers time to stop routing here

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
