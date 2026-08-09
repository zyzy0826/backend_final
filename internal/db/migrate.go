/* 啟動時自動套用資料表結構，不需要宿主機安裝 psql */

package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// schema is compiled into the binary so migrations need no files at run time.
//
//go:embed schema.sql
var schema string

// Migrate applies schema.sql to the connected database.
//
// Every statement in it is idempotent (CREATE TABLE IF NOT EXISTS /
// ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS), so running it on
// every startup is safe. Doing it here rather than through Postgres' initdb
// hook means an *existing* database also picks up schema changes — initdb only
// ever runs against a brand new data volume — and nothing has to be applied by
// hand with a host-side psql.
//
// pgx sends a statement with no arguments over the simple query protocol, which
// is what allows the whole multi-statement file to go in one round trip.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
