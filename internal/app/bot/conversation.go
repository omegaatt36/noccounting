package bot

import (
	"sync"
	"time"

	"github.com/omegaatt36/noccounting/domain"
)

type ConversationStep int

const (
	StepNone ConversationStep = iota
	StepEditSelect
	StepEditField
	StepEditValue
	ReceiptConfirm
)

type ConversationState struct {
	Step      ConversationStep
	StartedAt time.Time

	// Trip is the trip the conversation was started under. A flow files its
	// expense there even if the person switches trip halfway through it.
	Trip domain.Trip

	// For /edit flow
	EditingExpense *domain.Expense
	EditField      string

	// For receipt scanning flow
	ReceiptAnalysis *domain.ReceiptAnalysis
	ReceiptImage    []byte
	ReceiptCategory domain.Category
	ReceiptMethod   domain.PaymentMethod
}

const defaultConversationTTL = 15 * time.Minute

// ConversationManager is conversation state per user id.
type ConversationManager struct {
	states sync.Map // map[int64]*ConversationState (user ID -> state)
	ttl    time.Duration
}

func NewConversationManager() *ConversationManager {
	return NewConversationManagerWithTTL(defaultConversationTTL)
}

func NewConversationManagerWithTTL(ttl time.Duration) *ConversationManager {
	return &ConversationManager{ttl: ttl}
}

func (m *ConversationManager) GetState(userID int64) *ConversationState {
	val, ok := m.states.Load(userID)
	if !ok {
		return nil
	}
	state := val.(*ConversationState)
	if m.ttl > 0 && time.Since(state.StartedAt) > m.ttl {
		m.states.Delete(userID)
		return nil
	}
	return state
}

func (m *ConversationManager) SetState(userID int64, state *ConversationState) {
	if state != nil && state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	m.states.Store(userID, state)
}

func (m *ConversationManager) ClearState(userID int64) {
	m.states.Delete(userID)
}

// Take claims a person's conversation for a step that must run once, such as
// filing a receipt or deleting an expense: of two taps arriving together, only
// one gets the state. It claims nothing, and leaves the state be, when ok says
// the conversation is not the one the step belongs to.
func (m *ConversationManager) Take(userID int64, ok func(*ConversationState) bool) *ConversationState {
	val, loaded := m.states.Load(userID)
	if !loaded {
		return nil
	}
	state := val.(*ConversationState)
	if (m.ttl > 0 && time.Since(state.StartedAt) > m.ttl) || !ok(state) {
		return nil
	}
	if !m.states.CompareAndDelete(userID, val) {
		return nil
	}
	return state
}

func (m *ConversationManager) StartEditFlow(userID int64, trip domain.Trip, expense *domain.Expense) *ConversationState {
	state := &ConversationState{
		Step:           StepEditField,
		StartedAt:      time.Now(),
		Trip:           trip,
		EditingExpense: expense,
	}
	m.SetState(userID, state)
	return state
}
