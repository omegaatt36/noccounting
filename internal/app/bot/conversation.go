package bot

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omegaatt36/noccounting/domain"
)

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
	ReceiptConfirm
)

type ConversationState struct {
	Step      ConversationStep
	StartedAt time.Time

	// Trip is the trip the conversation was started under. A flow files its
	// expense there even if the person switches trip halfway through it.
	Trip domain.Trip

	// For /quick flow
	ExpenseDraft *domain.Expense

	// For /edit flow
	EditingExpense *domain.Expense
	EditField      string

	// For receipt scanning flow
	ReceiptAnalysis *domain.ReceiptAnalysis
	ReceiptImage    []byte
}

// ConversationManager is conversation state per user id.
type ConversationManager struct {
	states sync.Map // map[int64]*ConversationState (user ID -> state)
}

func NewConversationManager() *ConversationManager {
	return &ConversationManager{}
}

func (m *ConversationManager) GetState(userID int64) *ConversationState {
	if state, ok := m.states.Load(userID); ok {
		return state.(*ConversationState)
	}
	return nil
}

func (m *ConversationManager) SetState(userID int64, state *ConversationState) {
	m.states.Store(userID, state)
}

func (m *ConversationManager) ClearState(userID int64) {
	m.states.Delete(userID)
}

func (m *ConversationManager) StartQuickFlow(userID int64, backendUserID string, trip domain.Trip) *ConversationState {
	state := &ConversationState{
		Step:      StepQuickName,
		StartedAt: time.Now(),
		Trip:      trip,
		ExpenseDraft: &domain.Expense{
			PaidByID:     backendUserID,
			ShoppedAt:    time.Now(),
			ExchangeRate: decimal.Zero,
		},
	}
	m.SetState(userID, state)
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
