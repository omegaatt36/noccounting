package trek

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

const settlementPathSuffix = "/budget/settlement"

type settlementResponse struct {
	Balances []struct {
		UserID   int64   `json:"user_id"`
		Username string  `json:"username"`
		Balance  float64 `json:"balance"`
	} `json:"balances"`
	Flows []struct {
		From   settlementPerson `json:"from"`
		To     settlementPerson `json:"to"`
		Amount float64          `json:"amount"`
	} `json:"flows"`
	Currency    string `json:"currency"`
	Unconverted struct {
		ItemIDs       []int64 `json:"item_ids"`
		SettlementIDs []int64 `json:"settlement_ids"`
	} `json:"unconverted"`
}

type settlementPerson struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

func (r *tripRepo) Settlement(ctx context.Context) (*domain.Settlement, error) {
	var resp settlementResponse
	if err := r.client.do(ctx, http.MethodGet, tripPath(r.tripID, settlementPathSuffix), nil, &resp); err != nil {
		return nil, fmt.Errorf("reading the TREK settlement of trip %d: %w", r.tripID, err)
	}

	currency := r.baseCurrency
	if said := strings.ToUpper(strings.TrimSpace(resp.Currency)); said != "" && domain.Currency(said) != r.baseCurrency {
		return nil, fmt.Errorf("TREK settled trip %d in %s, but the trip is in %s", r.tripID, said, r.baseCurrency)
	}

	settlement := &domain.Settlement{
		Currency:    currency,
		Unconverted: len(resp.Unconverted.ItemIDs) + len(resp.Unconverted.SettlementIDs),
	}
	for _, balance := range resp.Balances {
		settlement.Balances = append(settlement.Balances, domain.Balance{
			UserID: trekUserID(balance.UserID),
			Name:   balance.Username,
			Amount: decimal.NewFromFloat(balance.Balance),
		})
	}
	for _, flow := range resp.Flows {
		settlement.Transfers = append(settlement.Transfers, domain.Transfer{
			FromID:   trekUserID(flow.From.UserID),
			FromName: flow.From.Username,
			ToID:     trekUserID(flow.To.UserID),
			ToName:   flow.To.Username,
			Amount:   decimal.NewFromFloat(flow.Amount),
		})
	}
	return settlement, nil
}

func (r *tripRepo) Members(ctx context.Context) ([]domain.Member, error) {
	if r.payers == nil {
		return nil, fmt.Errorf("%w: no roster is configured to name who is on trip %d", ErrInvalidExpense, r.tripID)
	}

	roster, err := r.payers.rosterFor(ctx)
	if err != nil {
		return nil, err
	}

	members := make([]domain.Member, 0, len(roster.members))
	serviceUserID := r.client.serviceUserID()
	for _, member := range roster.Members() {
		if member.UserID == serviceUserID {
			continue
		}
		members = append(members, domain.Member{
			ID:   strconv.FormatInt(member.UserID, 10),
			Name: member.Username,
		})
	}
	return members, nil
}
