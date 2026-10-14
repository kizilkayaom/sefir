package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Create a connection pool and verify database connectivity
func Open(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connectionString)

	if err != nil {
		return nil, err
	}

	err = pool.Ping(ctx)

	if err != nil {
		pool.Close()
		return nil, err
	} else {
		return pool, nil
	}
}
