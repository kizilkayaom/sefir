package main

import (
	"context"
	"errors"
	"flag"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/database"
	"log/slog"
	"os"
	"time"
)

func run(contactID string) error {
	if contactID == "" {
		return errors.New("contact-id is required")
	}

	connectionString := os.Getenv("DATABASE_URL")
	if connectionString == "" {
		return errors.New("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, connectionString)
	if err != nil {
		return err
	}

	defer pool.Close()
	contact, err := catalogue.GetConfirmedContact(ctx, pool, contactID)
	if err != nil {
		return err
	}

	slog.Info("Loaded confirmed contact", "contact_id", contact.ID)
	return nil
}

func main() {
	contactID := flag.String("contact-id", "", "Confirmed contact ID to fetch")
	flag.Parse()

	if err := run(*contactID); err != nil {
		slog.Error("Catalogue startup failed", "error", err)
		os.Exit(1)
	}
}
