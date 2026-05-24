// package scenarios lets an operator queue a forced response that a
// handler will return for the next matching microsoft graph request,
// instead of falling through to the normal mock behaviour. useful for
// proving any consumer app handles 401/403/429/5xx without driving the
// real failure through azure ad / graph.
//
// default policy is one-shot: the scenario is consumed by the first
// matching request and removed. sticky scenarios persist until cleared.
package scenarios

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// scenario is a queued forced response.
type Scenario struct {
	ID            string            `json:"id"`
	Description   string            `json:"description,omitempty"`
	Method        string            `json:"method"`
	PathContains  string            `json:"pathContains"`
	Status        int               `json:"status"`
	Headers       map[string]string `json:"headers,omitempty"`
	BodyJSON      string            `json:"bodyJson,omitempty"`
	OneShot       bool              `json:"oneShot"`
	CreatedAt     time.Time         `json:"createdAt"`
	ConsumedAt    *time.Time        `json:"consumedAt,omitempty"`
	ConsumedCount int               `json:"consumedCount"`
}

// store is a thread-safe collection of queued scenarios.
type Store struct {
	mu        sync.Mutex
	scenarios []*Scenario
}

func NewStore() *Store { return &Store{} }

func (s *Store) Add(sc *Scenario) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc.ID == "" {
		sc.ID = newID()
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = time.Now().UTC()
	}
	s.scenarios = append(s.scenarios, sc)
	return sc.ID
}

func (s *Store) All() []*Scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Scenario, len(s.scenarios))
	for i, sc := range s.scenarios {
		out[len(s.scenarios)-1-i] = sc
	}
	return out
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.scenarios {
		if sc.ID == id {
			s.scenarios = append(s.scenarios[:i], s.scenarios[i+1:]...)
			return true
		}
	}
	return false
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenarios = nil
}

// matchAndConsume finds the first scenario that matches the request.
// one-shot scenarios are removed after a match; sticky scenarios
// increment consumed count and remain.
func (s *Store) MatchAndConsume(method, path string) *Scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.scenarios {
		if sc.Method != "" && !strings.EqualFold(sc.Method, method) {
			continue
		}
		if sc.PathContains != "" && !strings.Contains(strings.ToLower(path), strings.ToLower(sc.PathContains)) {
			continue
		}
		now := time.Now().UTC()
		sc.ConsumedAt = &now
		sc.ConsumedCount++
		if sc.OneShot {
			s.scenarios = append(s.scenarios[:i], s.scenarios[i+1:]...)
		}
		return sc
	}
	return nil
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.scenarios)
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return "sc_" + hex.EncodeToString(b[:])
}
