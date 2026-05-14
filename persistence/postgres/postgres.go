// Package postgres provides Postgres-backed repository adapters via
// sqlc-generated queries on top of pgx/v5. The schema and the queries
// live alongside the wrapper:
//
//	migrations/  -- golang-migrate up/down SQL applied out of band
//	sqlc/        -- queries.sql, the sqlc input
//	pgstore/     -- sqlc-generated Go (DO NOT EDIT by hand)
//
// To regenerate pgstore after editing the schema or queries, run:
//
//	cd persistence/postgres && sqlc generate
//
// One file per domain: domain-specific repository code (struct,
// constructor, port implementation, queries, mappers) lives in
// <domain>.go (e.g. accounting.go). The generic plumbing kept in this
// file -- pool connection + io.Closer adapter -- is shared by every
// domain factory in the package.
package postgres

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectPool opens a pgxpool.Pool from dsn, pings it, and returns the
// pool alongside an io.Closer the caller defers to release it.
// Domain factories (see accounting.go) call this and wrap the pool in
// their repository implementation.
func connectPool(ctx context.Context, dsn string) (*pgxpool.Pool, io.Closer, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: connect: %w", err)
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
