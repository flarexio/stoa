// Package postgres provides Postgres infrastructure: connection pooling,
// migrations, and sqlc plumbing. Domain-specific repository implementations
// live in per-domain factory files (e.g. accounting.go).
//
// To regenerate pgstore after editing the schema or queries, run:
//
//	cd persistence/postgres && sqlc generate
package postgres

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectPool opens a pgxpool.Pool from dsn, pings it, and returns the
// pool alongside an io.Closer the caller defers to release it.
func ConnectPool(ctx context.Context, dsn string) (*pgxpool.Pool, io.Closer, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: connect pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, poolCloser{pool: pool}, nil
}

// poolCloser adapts *pgxpool.Pool to io.Closer; pgxpool.Pool.Close
// returns no error, so we report nil unconditionally.
type poolCloser struct {
	pool *pgxpool.Pool
}

func (c poolCloser) Close() error {
	c.pool.Close()
	return nil
}
