package trek

import (
	"log/slog"
	"strings"

	"github.com/omegaatt36/noccounting/domain"
)

func fromTrekCategory(slug string) domain.Category {
	if category, err := domain.ParseCategory(strings.ToLower(strings.TrimSpace(slug))); err == nil {
		return category
	}

	slog.Warn("no noccounting category matches this TREK cost category; reading it as other",
		"category", slug)
	return domain.CategoryOther
}
