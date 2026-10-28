// Package db holds database plumbing shared by slices.
package db

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies the pending goose migrations in fsys. Every slice owns its
// migrations and tracks them in its own version table ("<scope>_migrations"),
// so slices never share a migration sequence.
func Migrate(ctx context.Context, pool *pgxpool.Pool, scope string, fsys fs.FS) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys,
		goose.WithTableName(scope+"_migrations"))
	if err != nil {
		return fmt.Errorf("migrations %s: %w", scope, err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrations %s: %w", scope, err)
	}
	return nil
}
