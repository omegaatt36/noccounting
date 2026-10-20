package webapp

import (
	"testing"
	"time"
)

func TestReceiptStash_PhotoExpires(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	stash := newReceiptStash()
	stash.now = func() time.Time { return now }

	token := stash.put([]byte("photo"))
	if _, _, ok := stash.get(token); !ok {
		t.Fatal("a fresh photo is missing")
	}

	now = now.Add(receiptStashTTL + time.Second)
	if _, _, ok := stash.get(token); ok {
		t.Error("a photo older than the TTL is still there")
	}
}

func TestReceiptStash_OldestPhotoMakesRoom(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	stash := newReceiptStash()
	stash.now = func() time.Time { return now }

	var tokens []string
	for range receiptStashLimit + 1 {
		tokens = append(tokens, stash.put([]byte("photo")))
		now = now.Add(time.Second)
	}

	if _, _, ok := stash.get(tokens[0]); ok {
		t.Error("the oldest photo stayed when the stash was full")
	}
	if _, _, ok := stash.get(tokens[len(tokens)-1]); !ok {
		t.Error("the newest photo was dropped")
	}
	if len(stash.items) != receiptStashLimit {
		t.Errorf("%d photos kept, want the limit of %d", len(stash.items), receiptStashLimit)
	}
}

func TestSubmissions_ForgetsRequestsAfterTheTTL(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	seen := newSubmissions()
	seen.now = func() time.Time { return now }

	if _, first := seen.begin("req"); !first {
		t.Fatal("a new id was taken for a repeat")
	}
	if _, first := seen.begin("req"); first {
		t.Fatal("the same id was taken for new")
	}

	now = now.Add(submissionTTL + time.Second)
	if _, first := seen.begin("req"); !first {
		t.Error("an id older than the TTL is still remembered")
	}
}

func TestSubmissions_NoIDIsNeverARepeat(t *testing.T) {
	seen := newSubmissions()
	for range 3 {
		if _, first := seen.begin(""); !first {
			t.Fatal("a form without a request id was taken for a repeat")
		}
	}
}
