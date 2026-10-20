package webapp

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	receiptStashTTL   = 30 * time.Minute
	receiptStashLimit = 8
)

// receiptStash holds a scanned receipt's photo until its expense is saved, so an abandoned or repeated scan leaves nothing on the trip.
type receiptStash struct {
	mu    sync.Mutex
	items map[string]*stashedReceipt
	now   func() time.Time
}

type stashedReceipt struct {
	image []byte
	at    time.Time
	// fileID is the upload of image, so a retried save does not upload it again.
	fileID string
}

func newReceiptStash() *receiptStash {
	return &receiptStash{items: make(map[string]*stashedReceipt), now: time.Now}
}

func (s *receiptStash) put(image []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropExpired()
	for len(s.items) >= receiptStashLimit {
		var oldest string
		for token, item := range s.items {
			if oldest == "" || item.at.Before(s.items[oldest].at) {
				oldest = token
			}
		}
		delete(s.items, oldest)
	}

	token := uuid.NewString()
	s.items[token] = &stashedReceipt{image: image, at: s.now()}
	return token
}

func (s *receiptStash) get(token string) (image []byte, fileID string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropExpired()
	item, ok := s.items[token]
	if !ok {
		return nil, "", false
	}
	return item.image, item.fileID, true
}

func (s *receiptStash) setFile(token, fileID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.items[token]; ok {
		item.fileID = fileID
	}
}

func (s *receiptStash) discard(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, token)
}

func (s *receiptStash) dropExpired() {
	for token, item := range s.items {
		if s.now().Sub(item.at) > receiptStashTTL {
			delete(s.items, token)
		}
	}
}
