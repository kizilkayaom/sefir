package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/email"
)

func run() error {
	config, err := email.LoadOAuthConfig(
		"../credentials-gmail.json",
		"http://localhost:8080/oauth/google/callback",
	)

	if err != nil {
		return err
	}

	auth := &oauthServer{
		config:   config,
		attempts: make(map[string]oauthAttempt),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/google/start", auth.handleStart)
	mux.HandleFunc("GET /oauth/google/callback", auth.handleCallback)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:              "localhost:8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("Starting Gmail authorization server", "redirect_url", config.RedirectURL)

	return srv.ListenAndServe()
}

func main() {
	if err := run(); err != nil {
		slog.Error("HTTP server startup failed", "error", err)
		os.Exit(1)
	}
}
