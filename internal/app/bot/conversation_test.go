package bot_test

import (
	"sync"
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/domain"
	"github.com/omegaatt36/noccounting/internal/app/bot"
)

func TestConversationManager_ExpiresStateAfterTTL(t *testing.T) {
	mgr := bot.NewConversationManagerWithTTL(10 * time.Millisecond)

	mgr.SetState(12345, &bot.ConversationState{
		Step: bot.StepEditField,
	})

	if state := mgr.GetState(12345); state == nil {
		t.Fatal("expected state to be found immediately")
	}

	time.Sleep(20 * time.Millisecond)

	if state := mgr.GetState(12345); state != nil {
		t.Fatalf("expected state to have expired after TTL, got %+v", state)
	}
}

func TestConversationManager_TakeHandsTheStateToOnlyOneCaller(t *testing.T) {
	mgr := bot.NewConversationManager()
	mgr.SetState(7, &bot.ConversationState{Step: bot.ReceiptConfirm, ReceiptAnalysis: &domain.ReceiptAnalysis{}})
	isReceipt := func(s *bot.ConversationState) bool { return s.ReceiptAnalysis != nil }

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		winner int
	)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mgr.Take(7, isReceipt) != nil {
				mu.Lock()
				winner++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if winner != 1 {
		t.Fatalf("%d callers took the state, want exactly 1", winner)
	}
	if mgr.GetState(7) != nil {
		t.Error("the taken state is still there")
	}
}

func TestConversationManager_TakeLeavesAnotherConversationAlone(t *testing.T) {
	mgr := bot.NewConversationManager()
	mgr.SetState(7, &bot.ConversationState{Step: bot.StepEditField})

	if got := mgr.Take(7, func(s *bot.ConversationState) bool { return s.ReceiptAnalysis != nil }); got != nil {
		t.Fatalf("took %+v, want nothing from an edit conversation", got)
	}
	if mgr.GetState(7) == nil {
		t.Error("a claim for a receipt removed the edit conversation")
	}
}

func TestConversationManager_TakeIgnoresExpiredState(t *testing.T) {
	mgr := bot.NewConversationManagerWithTTL(10 * time.Millisecond)
	mgr.SetState(7, &bot.ConversationState{Step: bot.ReceiptConfirm})
	time.Sleep(20 * time.Millisecond)

	if got := mgr.Take(7, func(*bot.ConversationState) bool { return true }); got != nil {
		t.Fatalf("took an expired state: %+v", got)
	}
}
