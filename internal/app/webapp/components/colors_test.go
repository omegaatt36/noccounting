package components

import (
	"testing"

	"github.com/omegaatt36/noccounting/domain"
)

// A category without a color of its own would be drawn in the neutral gray the
// donut uses for something it does not know, and read as "other".
func TestCategoryColor_EveryCategoryHasItsOwn(t *testing.T) {
	seen := map[string]domain.Category{}
	for _, category := range domain.CategoryValues() {
		color, ok := categoryColors[category]
		if !ok {
			t.Errorf("%q has no color", category)
			continue
		}
		if other, dup := seen[color]; dup {
			t.Errorf("%q and %q share the color %s", category, other, color)
		}
		seen[color] = category
	}
}

func TestCategoryColor_UnknownSlugIsNeutral(t *testing.T) {
	if got := CategoryColor("souvenirs"); got != "#575653" {
		t.Errorf("CategoryColor() = %q, want the neutral gray", got)
	}
}
