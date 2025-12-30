package domain_test

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

func TestReceiptAnalysis_TotalMatchesItems(t *testing.T) {
	analysis := domain.ReceiptAnalysis{
		Items: []domain.ReceiptItem{
			{Name: "拉麵", Price: 1200, Category: domain.Category食},
			{Name: "餅乾禮盒", Price: 800, Category: domain.Category購},
		},
		Currency: domain.CurrencyJPY,
		Total:    2000,
	}

	var sum uint64
	for _, item := range analysis.Items {
		sum += item.Price
	}

	if sum != analysis.Total {
		t.Errorf("items sum %d != total %d", sum, analysis.Total)
	}

	if len(analysis.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(analysis.Items))
	}

	if analysis.Currency != domain.CurrencyJPY {
		t.Errorf("expected JPY, got %s", analysis.Currency)
	}
}

func TestReceiptItem_Fields(t *testing.T) {
	item := domain.ReceiptItem{
		Name:     "抹茶",
		Price:    350,
		Category: domain.Category食,
	}

	if item.Name != "抹茶" {
		t.Errorf("Name = %q, want %q", item.Name, "抹茶")
	}
	if item.Price != 350 {
		t.Errorf("Price = %d, want %d", item.Price, 350)
	}
	if item.Category != domain.Category食 {
		t.Errorf("Category = %q, want %q", item.Category, domain.Category食)
	}
}
