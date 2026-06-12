package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const schema = `
CREATE TABLE IF NOT EXISTS ledgers (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL UNIQUE,
    notion_database_id TEXT NOT NULL UNIQUE,
    is_active          INTEGER NOT NULL DEFAULT 0,
    created_at         INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_active ON ledgers(is_active) WHERE is_active = 1;
`

// migrate creates tables and indexes if they do not exist.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to run migration: %w", err)
	}
	return nil
}
