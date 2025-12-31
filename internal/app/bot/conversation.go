package bot

import (
	"sync"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/shopspring/decimal"
)

// ConversationStep represents the current step in a conversation flow.
type ConversationStep int

const (
	StepNone ConversationStep = iota
	StepQuickName
	StepQuickPrice
	StepQuickCurrency
	StepQuickCategory
	StepQuickMethod
	StepQuickConfirm
	StepEditSelect
	StepEditField
	StepEditValue
)

// ConversationState holds the state for an ongoing conversation.
type ConversationState struct {
	Step      ConversationStep
	StartedAt time.Time

	// For /quick flow
	ExpenseDraft *domain.Expense

	// For /edit flow
	EditingExpense *domain.Expense
	EditField      string
}

// ConversationManager manages conversation states for users.
type ConversationManager struct {
	states sync.Map // map[int64]*ConversationState (user ID -> state)
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager() *ConversationManager {
	return &ConversationManager{}
}

// GetState retrieves the conversation state for a user.
func (m *ConversationManager) GetState(userID int64) *ConversationState {
	if state, ok := m.states.Load(userID); ok {
		return state.(*ConversationState)
	}
	return nil
}

// SetState sets the conversation state for a user.
func (m *ConversationManager) SetState(userID int64, state *ConversationState) {
	m.states.Store(userID, state)
}

// ClearState removes the conversation state for a user.
func (m *ConversationManager) ClearState(userID int64) {
	m.states.Delete(userID)
}

// StartQuickFlow starts the quick expense creation flow.
func (m *ConversationManager) StartQuickFlow(userID int64, notionUserID string) *ConversationState {
	state := &ConversationState{
		Step:      StepQuickName,
		StartedAt: time.Now(),
		ExpenseDraft: &domain.Expense{
			PaidByID:     notionUserID,
			ShoppedAt:    time.Now(),
			ExchangeRate: decimal.Zero,
		},
	}
	m.SetState(userID, state)
	return state
}

// StartEditFlow starts the edit expense flow.
func (m *ConversationManager) StartEditFlow(userID int64, expense *domain.Expense) *ConversationState {
	state := &ConversationState{
		Step:           StepEditField,
		StartedAt:      time.Now(),
		EditingExpense: expense,
	}
	m.SetState(userID, state)
	return state
}
