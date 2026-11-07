package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/database"
	"github.com/kizilkayaom/sefir/mcp/internal/webapp"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func run() error {
	root, err := filepath.Abs(env("SEFIR_ROOT", ".."))
	if err != nil {
		return err
	}
	origin := env("SEFIR_WEB_ORIGIN", "http://localhost:8080")
	config := webapp.Config{
		Origin: origin, Owner: strings.TrimSpace(os.Getenv("SEFIR_OWNER")),
		SignupEnabled:     os.Getenv("SEFIR_SIGNUP_ENABLED") == "true",
		Password:          os.Getenv("SEFIR_OWNER_PASSWORD"),
		Dist:              env("SEFIR_WEB_DIST", filepath.Join(root, "web", "dist")),
		CredentialsPath:   filepath.Join(root, ".sefir", "owner.json"),
		OperatorAccountID: strings.TrimSpace(os.Getenv("SEFIR_OPERATOR_ACCOUNT_ID")),
		AI:                &webapp.AIConfiguration{Path: filepath.Join(root, ".sefir", "ai-settings.json")},
	}
	if index, err := os.Stat(filepath.Join(config.Dist, "index.html")); err != nil || !index.Mode().IsRegular() {
		return errors.New("web assets are missing; build the frontend or set SEFIR_WEB_DIST")
	}
	googleSignIn, googleErr := webapp.NewGoogleSignIn(filepath.Join(root, "credentials-gmail.json"), origin+"/oauth/google/sign-in/callback")
	if googleErr == nil {
		config.GoogleAuth = googleSignIn
	} else {
		slog.Info("Google sign-in is unavailable; configure credentials-gmail.json to enable it")
	}
	var gmailConnections sync.Map
	getGmailConnection := func(accountID string) (*webapp.GmailConnection, error) {
		tokenPath, err := workspace.GmailTokenPath(root, accountID)
		if err != nil {
			return nil, err
		}
		connection, _ := gmailConnections.LoadOrStore(accountID, &webapp.GmailConnection{
			CredentialsPath: filepath.Join(root, "credentials-gmail.json"),
			TokenPath:       tokenPath, RedirectURL: origin + "/oauth/google/callback",
		})
		return connection.(*webapp.GmailConnection), nil
	}
	config.GmailForAccount = func(accountID string) (webapp.Gmail, error) {
		return getGmailConnection(accountID)
	}
	bridge := webapp.PythonBridge{
		Python: env("SEFIR_PYTHON", filepath.Join(root, "agent", ".venv", "bin", "python")),
		Root:   root,
		AI:     config.AI,
	}
	if os.Getenv("DATABASE_URL") == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	pool, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	cancel()
	if err != nil {
		return errors.New("database connection failed; check DATABASE_URL and PostgreSQL")
	}
	defer pool.Close()
	config.Accounts = webapp.PostgresAccounts{Pool: pool}
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = pool.Exec(checkCtx, "SELECT account_id FROM accounts LIMIT 0")
	checkCancel()
	if err != nil {
		return errors.New("account database is not ready; apply migration 00007_add_accounts.sql")
	}
	app, err := webapp.New(config, webapp.PostgresStore{Pool: pool}, bridge, nil)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: env("SEFIR_LISTEN_ADDR", "127.0.0.1:8080"), Handler: app.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 160 * time.Second, IdleTimeout: 60 * time.Second,
	}
	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopping.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	slog.Info("Starting Sefir web server", "origin", origin)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	if err := run(); err != nil {
		slog.Error("Web startup failed", "error", err)
		os.Exit(1)
	}
}
