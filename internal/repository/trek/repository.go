package trek

import (
	"context"
	"sync"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/service/expense"
)

type Repo struct {
	client  *Client
	rates   rateProvider
	mu      sync.Mutex
	rosters map[int64]*PayerResolver
}

var _ expense.AccountingRepo = (*Repo)(nil)

func NewRepo(client *Client, rates rateProvider) *Repo {
	return &Repo{client: client, rates: rates, rosters: make(map[int64]*PayerResolver)}
}

func (r *Repo) forTrip(trip domain.Trip) *tripRepo {
	r.mu.Lock()
	defer r.mu.Unlock()
	resolver := r.rosters[trip.ID]
	if resolver == nil {
		resolver = NewPayerResolver(r.client, trip.ID)
		r.rosters[trip.ID] = resolver
	}
	return newTripRepo(r.client, trip, resolver, r.rates)
}

func (r *Repo) CreateExpense(ctx context.Context, trip domain.Trip, e *domain.Expense) error {
	return r.forTrip(trip).CreateExpense(ctx, e)
}

func (r *Repo) QueryExpenses(ctx context.Context, trip domain.Trip) ([]domain.Expense, error) {
	return r.forTrip(trip).QueryExpenses(ctx)
}

func (r *Repo) QueryExpensesWithFilter(ctx context.Context, trip domain.Trip, filter expense.ExpenseFilter) ([]domain.Expense, error) {
	return r.forTrip(trip).QueryExpensesWithFilter(ctx, filter)
}

func (r *Repo) UpdateExpense(ctx context.Context, trip domain.Trip, e *domain.Expense) error {
	return r.forTrip(trip).UpdateExpense(ctx, e)
}

func (r *Repo) DeleteExpense(ctx context.Context, trip domain.Trip, id string) error {
	return r.forTrip(trip).DeleteExpense(ctx, id)
}

func (r *Repo) Settlement(ctx context.Context, trip domain.Trip) (*domain.Settlement, error) {
	return r.forTrip(trip).Settlement(ctx)
}

func (r *Repo) Members(ctx context.Context, trip domain.Trip) ([]domain.Member, error) {
	return r.forTrip(trip).Members(ctx)
}

func (r *Repo) UploadFile(ctx context.Context, trip domain.Trip, filePath string) (string, error) {
	return r.forTrip(trip).UploadFile(ctx, filePath)
}
