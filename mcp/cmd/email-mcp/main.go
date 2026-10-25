package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/database"
	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func run() error {

	connectionString := os.Getenv("DATABASE_URL")
	if connectionString == "" {
		return errors.New("DATABASE_URL is required")
	}

	startupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := database.Open(startupCtx, connectionString)
	if err != nil {
		return err
	}

	defer pool.Close()

	config, err := email.LoadOAuthConfig(
		"../credentials-gmail.json",
		"http://localhost:8080/oauth/google/callback",
	)
	if err != nil {
		return fmt.Errorf("load Gmail OAuth configuration: %w", err)
	}

	client, err := email.NewGmailClient(context.Background(), config, "../token-gmail.json")
	if err != nil {
		return fmt.Errorf("create Gmail client: %w", err)
	}

	profileCtx, cancelProfile := context.WithTimeout(context.Background(), 30*time.Second)
	profile, err := email.GetGmailProfile(profileCtx, client)
	cancelProfile()
	if err != nil {
		return fmt.Errorf("get Gmail profile: %w", err)
	}

	_, messageIDDomain, ok := strings.Cut(profile.EmailAddress, "@")
	if !ok || messageIDDomain == "" {
		return errors.New("Gmail profile email is missing its domain")
	}

	slog.Info("Connected Gmail account", "email", profile.EmailAddress)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "email-mcp",
		Version: "0.1.0",
	}, nil)

	email.RegisterTools(
		server, pool, client, profile.EmailAddress, messageIDDomain,
	)

	slog.Info("Starting email MCP server")
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func main() {

	if err := run(); err != nil {
		slog.Error("Email startup failed", "error", err)
		os.Exit(1)
	}
}
