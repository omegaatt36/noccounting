package webapp

import (
	"sync"
	"time"
)

const submissionTTL = 10 * time.Minute

// submissions remembers forms by request id, so a form re-sent after a lost answer files nothing twice.
type submissions struct {
	mu   sync.Mutex
	seen map[string]*submission
	now  func() time.Time
}

type submission struct {
	at     time.Time
	done   bool
	result resultData
}

func newSubmissions() *submissions {
	return &submissions{seen: make(map[string]*submission), now: time.Now}
}

func (s *submissions) begin(id string) (prev submission, first bool) {
	if id == "" {
		return submission{}, true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, seen := range s.seen {
		if s.now().Sub(seen.at) > submissionTTL {
			delete(s.seen, key)
		}
	}
	if seen, ok := s.seen[id]; ok {
		return *seen, false
	}
	s.seen[id] = &submission{at: s.now()}
	return submission{}, true
}

func (s *submissions) finish(id string, result resultData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seen, ok := s.seen[id]; ok {
		seen.done, seen.result = true, result
	}
}

func (s *submissions) abandon(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, id)
}
