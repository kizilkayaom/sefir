package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/email"
)

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := email.LoadOAuthConfig(
		"../credentials-gmail.json",
		"http://localhost:8080/oauth/google/callback",
	)
	if err != nil {
		return fmt.Errorf("load Gmail OAuth configuration: %w", err)
	}

	client, err := email.NewGmailClient(ctx, config, "../token-gmail.json")
	if err != nil {
		return fmt.Errorf("create Gmail client: %w", err)
	}

	profile, err := email.GetGmailProfile(ctx, client)
	if err != nil {
		return fmt.Errorf("get Gmail profile: %w", err)
	}

	fmt.Println("Connected Gmail account:", profile.EmailAddress)
	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("Gmail connection check failed", "error", err)
		os.Exit(1)
	}
}
