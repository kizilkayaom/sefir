package main

import (
	"context"
	"errors"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/database"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log/slog"
	"os"
	"time"
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
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "catalogue-mcp",
		Version: "0.1.0",
	}, nil)

	catalogue.RegisterTools(server, pool)

	slog.Info("Starting catalogue MCP server")
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func main() {

	if err := run(); err != nil {
		slog.Error("Catalogue startup failed", "error", err)
		os.Exit(1)
	}
}
