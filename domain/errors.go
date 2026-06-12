package domain

import "errors"

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrNoActiveLedger = errors.New("no active ledger")
	ErrLedgerNotFound = errors.New("ledger not found")
	ErrLedgerExists   = errors.New("ledger with same name or database id already exists")
)
