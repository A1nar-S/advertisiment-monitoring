// Package db holds database plumbing shared by slices.
package db

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Open connects to Postgres and checks the connection.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return pool, nil
}

// Migrate applies the pending goose migrations in fsys. Every slice owns its
// migrations and tracks them in its own version table ("<scope>_migrations"),
// so slices never share a migration sequence. A Postgres advisory lock lets
// several services migrate the same slice when they start together.
func Migrate(ctx context.Context, pool *pgxpool.Pool, scope string, fsys fs.FS) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrations %s: %w", scope, err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys,
		goose.WithTableName(scope+"_migrations"), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrations %s: %w", scope, err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrations %s: %w", scope, err)
	}
	return nil
}
