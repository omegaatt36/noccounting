package trek

import (
	"slices"
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

var trekCostCategories = []string{
	"accommodation",
	"food",
	"groceries",
	"transport",
	"flights",
	"activities",
	"sightseeing",
	"shopping",
	"fees",
	"health",
	"tips",
	"fuel",
	"parking",
	"other",
}

func TestDomainCategories_AreTheSetTREKRenders(t *testing.T) {
	var domainSlugs []string
	for _, category := range domain.CategoryValues() {
		domainSlugs = append(domainSlugs, string(category))
	}

	slices.Sort(domainSlugs)
	want := slices.Clone(trekCostCategories)
	slices.Sort(want)

	if !slices.Equal(domainSlugs, want) {
		t.Errorf("domain categories = %v, want TREK's %v", domainSlugs, want)
	}
}

func TestFromTrekCategory(t *testing.T) {
	tests := []struct {
		name string
		slug string
		want domain.Category
	}{
		{name: "a slug the domain holds", slug: "flights", want: domain.CategoryFlights},
		{name: "case and spaces folded", slug: "  Food ", want: domain.CategoryFood},
		{name: "free text from an older TREK", slug: "Souvenirs", want: domain.CategoryOther},
		{name: "empty", slug: "", want: domain.CategoryOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromTrekCategory(tt.slug); got != tt.want {
				t.Errorf("fromTrekCategory(%q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}
