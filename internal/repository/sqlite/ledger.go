package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/ledger"
)

// Repo implements ledger.LedgerRepo backed by a local SQLite file.
type Repo struct {
	db *sql.DB
}

// Ensure Repo implements ledger.LedgerRepo at compile time.
var _ ledger.LedgerRepo = (*Repo)(nil)

// NewRepo opens (creating if needed) the SQLite database, runs migrations and
// returns a ready-to-use Repo.
func NewRepo(path string) (*Repo, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer; avoids "database is locked"
	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		return nil, err
	}
	return &Repo{db: db}, nil
}

// Close closes the underlying database handle.
func (r *Repo) Close() error {
	return r.db.Close()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// List returns all ledgers ordered by id.
func (r *Repo) List(ctx context.Context) ([]domain.Ledger, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, notion_database_id, is_active, created_at FROM ledgers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list ledgers: %w", err)
	}
	defer rows.Close()

	var out []domain.Ledger
	for rows.Next() {
		l, err := scanLedger(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger row: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Add inserts a new ledger. The first ledger inserted becomes active,
// determined atomically within a transaction to avoid a race between
// counting existing ledgers and the insert.
func (r *Repo) Add(ctx context.Context, l *domain.Ledger) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledgers`).Scan(&count); err != nil {
		return fmt.Errorf("failed to count ledgers: %w", err)
	}
	active := 0
	if count == 0 {
		active = 1
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledgers (name, notion_database_id, is_active, created_at) VALUES (?, ?, ?, ?)`,
		l.Name, l.NotionDatabaseID, active, l.CreatedAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrLedgerExists
		}
		return fmt.Errorf("failed to add ledger: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	l.ID = uint(id)
	l.IsActive = active == 1

	return tx.Commit()
}

// GetActive returns the active ledger or domain.ErrNoActiveLedger.
func (r *Repo) GetActive(ctx context.Context) (*domain.Ledger, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, notion_database_id, is_active, created_at FROM ledgers WHERE is_active = 1`)
	l, err := scanLedger(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNoActiveLedger
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active ledger: %w", err)
	}
	return &l, nil
}

// GetByName returns the named ledger or domain.ErrLedgerNotFound.
func (r *Repo) GetByName(ctx context.Context, name string) (*domain.Ledger, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, notion_database_id, is_active, created_at FROM ledgers WHERE name = ?`, name)
	l, err := scanLedger(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrLedgerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get ledger by name: %w", err)
	}
	return &l, nil
}

// SetActiveByName atomically clears the current active flag and sets the named
// ledger active. Returns domain.ErrLedgerNotFound when the name does not exist.
func (r *Repo) SetActiveByName(ctx context.Context, name string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE ledgers SET is_active = 0 WHERE is_active = 1`); err != nil {
		return fmt.Errorf("failed to clear active: %w", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE ledgers SET is_active = 1 WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to set active: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrLedgerNotFound
	}
	return tx.Commit()
}

// SeedIfEmpty inserts a first active ledger only when the table is empty.
// Runs the count-check and insert in a single transaction to avoid a race
// with concurrent callers.
// Returns true when a row was seeded.
func (r *Repo) SeedIfEmpty(ctx context.Context, name, notionDatabaseID string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledgers`).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count ledgers: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledgers (name, notion_database_id, is_active, created_at) VALUES (?, ?, 1, ?)`,
		name, notionDatabaseID, now.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			// Another caller seeded between the count check and this insert.
			return false, nil
		}
		return false, fmt.Errorf("failed to insert seed ledger: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if n == 0 {
		return false, nil
	}

	return true, tx.Commit()
}

type scanner interface{ Scan(dest ...any) error }

func scanLedger(s scanner) (domain.Ledger, error) {
	var (
		l         domain.Ledger
		active    int
		createdAt int64
	)
	if err := s.Scan(&l.ID, &l.Name, &l.NotionDatabaseID, &active, &createdAt); err != nil {
		return domain.Ledger{}, err
	}
	l.IsActive = active == 1
	l.CreatedAt = time.Unix(createdAt, 0).UTC()
	return l, nil
}
