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

type MemberInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserInfo struct {
	Nickname   string `json:"nickname"`
	TelegramID int64  `json:"telegram_id"`
}

type BootstrapResponse struct {
	Users []UserInfo `json:"users"`
	// Current is zero, with no Trips and no Members, when there is no trip.
	Trips   []TripInfo   `json:"trips"`
	Current int64        `json:"current"`
	Members []MemberInfo `json:"members"`
}

// RateInfo carries rates as decimal strings so no precision is lost on the way to the page.
type RateInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rate string `json:"rate"`
}

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

func (h *Handler) nicknames() map[string]string {
	users, err := h.userService.GetAllUsers()
	if err != nil {
		slog.Warn("Failed to read the users for their nicknames", "error", err)
		return map[string]string{}
	}
	return nicknamesOf(users)
}
