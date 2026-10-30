package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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

	root := os.Getenv("SEFIR_ROOT")
	if root == "" {
		root = ".."
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "email-mcp",
		Version: "0.1.0",
	}, nil)

	email.RegisterAccountTools(server, pool, email.WorkspaceDeliveryAccounts(pool, root))

	slog.Info("Starting email MCP server")
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func main() {

	if err := run(); err != nil {
		slog.Error("Email startup failed", "error", err)
		os.Exit(1)
	}
}
