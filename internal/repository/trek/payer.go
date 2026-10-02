package trek

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

const (
	membersPathSuffix = "/members"

	rosterTTL = 5 * time.Minute
)

var (
	ErrInvalidPayerID = errors.New("the mapped user id is not a TREK user id")

	ErrPayerNotOnTrip = fmt.Errorf("payer is not a member of the trip: %w", domain.ErrNotOnTrip)

	ErrParticipantNotOnTrip = fmt.Errorf("participant is not a member of the trip: %w", domain.ErrNotOnTrip)

	ErrPayerAmountUnset = errors.New("a payer needs a positive amount in the expense currency")
)

type PayerRef struct {
	ID       string
	Nickname string
}

type Payer struct {
	UserID   int64
	Nickname string
}

type TrekPayer struct {
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
}

type RosterMember struct {
	UserID   int64
	Username string
	Role     string
}

type Roster struct {
	members map[int64]RosterMember
}

func (r *Roster) Contains(userID int64) bool {
	_, ok := r.members[userID]
	return ok
}

func (r *Roster) Member(userID int64) (RosterMember, bool) {
	member, ok := r.members[userID]
	return member, ok
}

func (r *Roster) Members() []RosterMember {
	members := make([]RosterMember, 0, len(r.members))
	for _, member := range r.members {
		members = append(members, member)
	}
	slices.SortFunc(members, func(a, b RosterMember) int { return cmp.Compare(a.UserID, b.UserID) })
	return members
}

func (r *Roster) String() string {
	members := r.Members()
	if len(members) == 0 {
		return "nobody"
	}

	described := make([]string, 0, len(members))
	for _, member := range members {
		described = append(described, fmt.Sprintf("%d %s (%s)", member.UserID, member.Username, member.Role))
	}
	return strings.Join(described, ", ")
}

type PayerResolver struct {
	client    *Client
	tripID    int64
	rosterTTL time.Duration

	// Serializes fetches so concurrent requests share one roster refresh.
	mu        sync.Mutex
	roster    *Roster
	fetchedAt time.Time
}

func NewPayerResolver(client *Client, tripID int64) *PayerResolver {
	return &PayerResolver{client: client, tripID: tripID, rosterTTL: rosterTTL}
}

func (r *PayerResolver) Resolve(ctx context.Context, ref PayerRef) (*Payer, error) {
	userID, err := parseTrekUserID(ref)
	if err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, nil
	}

	roster, err := r.rosterFor(ctx)
	if err != nil {
		return nil, err
	}

	if !roster.Contains(userID) {
		roster, err = r.refreshRoster(ctx)
		if err != nil {
			return nil, err
		}
		if !roster.Contains(userID) {
			return nil, r.notOnTrip(ref, userID, roster)
		}
	}

	return &Payer{UserID: userID, Nickname: ref.Nickname}, nil
}

func (r *PayerResolver) Participants(ctx context.Context, ids []string) ([]int64, error) {
	roster, err := r.rosterFor(ctx)
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []int64{}, nil
	}

	participants := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		userID, err := parseTrekUserID(PayerRef{ID: id})
		if err != nil {
			return nil, err
		}
		if userID == 0 {
			return nil, fmt.Errorf("%w: a participant names no user", ErrInvalidPayerID)
		}

		if !roster.Contains(userID) {
			roster, err = r.refreshRoster(ctx)
			if err != nil {
				return nil, err
			}
			if !roster.Contains(userID) {
				return nil, fmt.Errorf("%w: TREK user %d is not on trip %d. On the trip: %s",
					ErrParticipantNotOnTrip, userID, r.tripID, roster)
			}
		}

		if _, dup := seen[userID]; dup {
			continue
		}
		seen[userID] = struct{}{}
		participants = append(participants, userID)
	}
	return participants, nil
}

func PayerInput(payer *Payer, amount float64) ([]TrekPayer, error) {
	if payer == nil {
		return nil, nil
	}
	if !isPositiveNumber(amount) {
		return nil, fmt.Errorf("%w: payer %d would be written with the amount %v",
			ErrPayerAmountUnset, payer.UserID, amount)
	}
	return []TrekPayer{{UserID: payer.UserID, Amount: amount}}, nil
}

func parseTrekUserID(ref PayerRef) (int64, error) {
	raw := strings.TrimSpace(ref.ID)
	if raw == "" {
		return 0, nil
	}

	userID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q in the user mapping is not a number", ErrInvalidPayerID, ref.ID)
	}
	if userID <= 0 {
		return 0, fmt.Errorf("%w: %q in the user mapping is not a TREK user id", ErrInvalidPayerID, ref.ID)
	}
	return userID, nil
}

func (r *PayerResolver) rosterFor(ctx context.Context) (*Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.roster != nil && time.Since(r.fetchedAt) < r.rosterTTL {
		return r.roster, nil
	}
	return r.fetchRoster(ctx)
}

func (r *PayerResolver) refreshRoster(ctx context.Context) (*Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.fetchRoster(ctx)
}

func (r *PayerResolver) fetchRoster(ctx context.Context) (*Roster, error) {
	var resp tripMembers
	if err := r.client.do(ctx, http.MethodGet, tripPath(r.tripID, membersPathSuffix), nil, &resp); err != nil {
		return nil, fmt.Errorf("trek trip roster: %w", err)
	}

	roster, err := newRoster(resp, r.tripID)
	if err != nil {
		return nil, err
	}

	r.roster, r.fetchedAt = roster, time.Now()
	slog.Debug("read the TREK trip roster", "trip_id", r.tripID, "users", len(roster.members))
	return roster, nil
}

func (r *PayerResolver) notOnTrip(ref PayerRef, userID int64, roster *Roster) error {
	tripID := r.tripID
	slog.Warn("refusing a payer who is not on the trip",
		"trip_id", tripID, "trek_user_id", userID, "nickname", ref.Nickname)

	return fmt.Errorf("%w: %s is not on trip %d; add them to the trip in TREK or correct their id in USER_MAPPING. On the trip: %s",
		ErrPayerNotOnTrip, describePayer(ref, userID), tripID, roster)
}

func describePayer(ref PayerRef, userID int64) string {
	if ref.Nickname == "" {
		return fmt.Sprintf("TREK user %d", userID)
	}
	return fmt.Sprintf("%s (TREK user %d)", ref.Nickname, userID)
}

type tripMembers struct {
	Owner   rosterUser   `json:"owner"`
	Members []rosterUser `json:"members"`
}

type rosterUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// TREK returns the owner separately from the members array.
func newRoster(from tripMembers, tripID int64) (*Roster, error) {
	if from.Owner.ID <= 0 {
		return nil, fmt.Errorf("trek trip roster for trip %d names no owner, so no user on it can be a payer", tripID)
	}

	users := make([]rosterUser, 0, len(from.Members)+1)
	users = append(users, from.Owner)
	users = append(users, from.Members...)

	roster := &Roster{members: make(map[int64]RosterMember, len(users))}
	for _, user := range users {
		roster.members[user.ID] = RosterMember{
			UserID:   user.ID,
			Username: user.Username,
			Role:     user.Role,
		}
	}
	return roster, nil
}
