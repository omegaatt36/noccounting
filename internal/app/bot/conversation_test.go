package bot_test

import (
	"testing"
	"time"

	"github.com/omegaatt36/noccounting/internal/app/bot"
)

func TestConversationManager_ExpiresStateAfterTTL(t *testing.T) {
	mgr := bot.NewConversationManagerWithTTL(10 * time.Millisecond)

	mgr.SetState(12345, &bot.ConversationState{
		Step: bot.StepQuickName,
	})

	if state := mgr.GetState(12345); state == nil {
		t.Fatal("expected state to be found immediately")
	}

	time.Sleep(20 * time.Millisecond)

	if state := mgr.GetState(12345); state != nil {
		t.Fatalf("expected state to have expired after TTL, got %+v", state)
	}
}

func TestConversationManager_CleanupRemovesExpiredStates(t *testing.T) {
	mgr := bot.NewConversationManagerWithTTL(10 * time.Millisecond)

	mgr.SetState(1, &bot.ConversationState{Step: bot.StepQuickName})
	mgr.SetState(2, &bot.ConversationState{Step: bot.StepQuickPrice})

	time.Sleep(20 * time.Millisecond)

	mgr.Cleanup()

	if state := mgr.GetState(1); state != nil {
		t.Error("expected state 1 to be cleaned up")
	}
	if state := mgr.GetState(2); state != nil {
		t.Error("expected state 2 to be cleaned up")
	}
}
