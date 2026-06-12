package domain

import (
	"errors"
	"strings"
	"time"
)

// Ledger represents a registered Notion database that can be used as the
// active accounting ledger.
type Ledger struct {
	ID               uint
	Name             string
	NotionDatabaseID string
	IsActive         bool
	CreatedAt        time.Time
}

// Validate checks that required fields are present.
func (l Ledger) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("ledger name is required")
	}
	if strings.TrimSpace(l.NotionDatabaseID) == "" {
		return errors.New("notion database id is required")
	}
	return nil
}
