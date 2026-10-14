package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/omegaatt36/noccounting/internal/app/format"
	"github.com/omegaatt36/noccounting/internal/service/trip"
)

type TripInfo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Label    string `json:"label"`
	Currency string `json:"currency"`
}

// MemberInfo is someone an expense can be shared with.
type MemberInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserInfo is someone who can be named as the payer of an expense.
type UserInfo struct {
	Nickname   string `json:"nickname"`
	TelegramID int64  `json:"telegram_id"`
}

// BootstrapResponse is everything the page needs to draw itself once the caller
// is known to be allowed in, in one answer.
type BootstrapResponse struct {
	// Users are the people in the user mapping, whom a payer is picked from.
	Users []UserInfo `json:"users"`
	// Trips are the trips the caller can file under and Current the one they are
	// on; Current is zero, with no Trips and no Members, when there is none.
	Trips   []TripInfo `json:"trips"`
	Current int64      `json:"current"`
	// Members are who is on the current trip.
	Members []MemberInfo `json:"members"`
}

// RateInfo is what one unit of From costs in To, as a decimal string so no
// precision is lost on the way to the page.
type RateInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rate string `json:"rate"`
}

// RatesResponse is every quote, both directions of the pair.
type RatesResponse struct {
	Quotes []RateInfo `json:"quotes"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("Failed to encode a JSON response", "error", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// handleBootstrap answers who may use the app and what they find in it, so the
// page learns that, and draws its pickers, in one round trip. A caller who is
// refused gets the 403 of requireAuth; a TREK that cannot be read gets a 500,
// which the page offers a retry for.
func (h *Handler) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	trips, err := h.tripService.List(ctx)
	if err != nil {
		slog.Error("Failed to list the trips", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to list trips")
		return
	}

	response := BootstrapResponse{
		Users:   h.userInfos(),
		Trips:   make([]TripInfo, 0, len(trips)),
		Members: []MemberInfo{},
	}
	for _, candidate := range trips {
		response.Trips = append(response.Trips, TripInfo{
			ID:       candidate.ID,
			Title:    candidate.Title,
			Label:    format.Trip(candidate),
			Currency: candidate.Currency.String(),
		})
	}

	current, err := h.tripService.Current(ctx, caller)
	switch {
	case err == nil:
		response.Current = current.ID
	case errors.Is(err, trip.ErrNoTrip):
		// An empty list says so on its own.
		writeJSON(w, http.StatusOK, response)
		return
	default:
		slog.Error("Failed to read the current trip", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to read the current trip")
		return
	}

	members, err := h.expenseService.Members(ctx, current)
	if err != nil {
		slog.Error("Failed to read the trip members", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to read the trip members")
		return
	}

	// Someone in the user mapping goes by their nickname; anyone else by the
	// name TREK has for them.
	nicknames := h.nicknames()
	for _, member := range members {
		name := member.Name
		if nickname, ok := nicknames[member.ID]; ok {
			name = nickname
		}
		response.Members = append(response.Members, MemberInfo{ID: member.ID, Name: name})
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) userInfos() []UserInfo {
	users, err := h.userService.GetAllUsers()
	if err != nil {
		slog.Warn("Failed to read the users for the payer picker", "error", err)
		return []UserInfo{}
	}
	infos := make([]UserInfo, 0, len(users))
	for _, u := range users {
		infos = append(infos, UserInfo{Nickname: u.Nickname, TelegramID: u.TelegramID})
	}
	return infos
}

// handleSelectTrip makes a trip the caller's current one, the same choice the
// bot's /trip makes, so the two stay on one trip.
func (h *Handler) handleSelectTrip(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	tripID, err := strconv.ParseInt(r.FormValue("trip_id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid trip_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	selected, err := h.tripService.Select(ctx, caller, tripID)
	if err != nil {
		if errors.Is(err, trip.ErrTripNotFound) {
			writeJSONError(w, http.StatusNotFound, "trip not found")
			return
		}
		slog.Error("Failed to select a trip", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to select the trip")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"current": selected.ID})
}

func (h *Handler) handleGetRates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAuth(w, r); !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	quotes := h.expenseService.ExchangeRates(ctx)
	response := RatesResponse{Quotes: make([]RateInfo, 0, len(quotes))}
	for _, quote := range quotes {
		response.Quotes = append(response.Quotes, RateInfo{
			From: quote.From.String(),
			To:   quote.To.String(),
			Rate: quote.Rate.String(),
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// nicknames maps a backend user id to the name people in the user mapping go by.
func (h *Handler) nicknames() map[string]string {
	names := map[string]string{}
	users, err := h.userService.GetAllUsers()
	if err != nil {
		slog.Warn("Failed to read the users for their nicknames", "error", err)
		return names
	}
	for _, u := range users {
		names[u.BackendUserID] = u.Nickname
	}
	return names
}
