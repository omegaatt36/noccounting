package trip

import (
	"context"

	"github.com/omegaatt36/noccounting/domain"
)

type Repository interface {
	ListTrips(ctx context.Context) ([]domain.Trip, error)
}
