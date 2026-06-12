package domain_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestLedger_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ledger  domain.Ledger
		wantErr bool
	}{
		{"valid", domain.Ledger{Name: "2026 東京", NotionDatabaseID: "db-123"}, false},
		{"empty name", domain.Ledger{Name: "", NotionDatabaseID: "db-123"}, true},
		{"blank name", domain.Ledger{Name: "   ", NotionDatabaseID: "db-123"}, true},
		{"empty db id", domain.Ledger{Name: "x", NotionDatabaseID: ""}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ledger.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
