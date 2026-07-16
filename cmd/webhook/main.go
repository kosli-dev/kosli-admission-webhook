// Kosli admission webhook entrypoint.
//
// See internal/config for the full list of environment variables.
package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kosli-dev/kosli-admission-webhook/internal/admission"
	"github.com/kosli-dev/kosli-admission-webhook/internal/config"
	"github.com/kosli-dev/kosli-admission-webhook/internal/kosli"
	"github.com/kosli-dev/kosli-admission-webhook/internal/resolver"
	"github.com/kosli-dev/kosli-admission-webhook/internal/tlsreload"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// logger not built yet; write plainly and exit non-zero
		os.Stderr.WriteString("configuration error: " + err.Error() + "\n")
		os.Exit(1)
	}
	log := cfg.Logger()
	log.Info("starting kosli-webhook",
		"org", cfg.Org,
		"scope", cfg.Scope(),
		"cacheTTL", cfg.CacheTTL.String(),
		"digestPinning", cfg.RequireDigestPinning,
		"denyUnknown", cfg.DenyUnknownArtifacts,
		"listen", cfg.ListenAddr,
	)

	srv := &admission.Server{
		Cfg:      cfg,
		Kosli:    kosli.New(cfg, log),
		Resolver: resolver.New(cfg.CacheTTL),
		Log:      log,
	}

	reloader, err := tlsreload.New(cfg.CertFile, cfg.KeyFile, log)
	if err != nil {
		log.Error("cannot load TLS keypair", "error", err)
		os.Exit(1)
	}

	// ready flips to false on SIGTERM so /readyz fails and the pod drops
	// out of Service endpoints before we stop serving.
	var ready atomic.Bool
	ready.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("/validate", srv.Validate)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // liveness: process is up
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	httpSrv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: reloader.GetCertificate,
		},
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServeTLS("", "") }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		log.Error("server failed", "error", err)
		os.Exit(1)
	case sig := <-sigCh:
		log.Info("shutdown signal received", "signal", sig.String())
	}

	// Drain sequence: mark unready, wait for endpoint/kube-proxy propagation,
	// then finish in-flight admission reviews.
	ready.Store(false)
	log.Info("draining", "delay", cfg.ShutdownDelay.String())
	time.Sleep(cfg.ShutdownDelay)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown incomplete", "error", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
