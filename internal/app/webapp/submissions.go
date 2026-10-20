package webapp

import (
	"sync"
	"time"
)

const submissionTTL = 10 * time.Minute

// submissions remembers the expense forms it has seen by the request id each
// carries. A form sent again with the id of one that already went through, as
// happens when the answer was lost on a bad connection, is told so instead of
// filing the expense twice.
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

// begin claims a request id. When it is not the first to arrive, prev says what
// became of the earlier one: done with its result, or still being worked on.
// No id is never a repeat.
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

// finish records that a request went through, with the answer it was given.
func (s *submissions) finish(id string, result resultData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seen, ok := s.seen[id]; ok {
		seen.done, seen.result = true, result
	}
}

// abandon frees the id of a request that failed, so the form can send it again.
func (s *submissions) abandon(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, id)
}
