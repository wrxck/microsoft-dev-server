// Package store provides an in-memory ring-buffer store for captured
// Microsoft Graph API requests. Safe for concurrent use.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Mail captures one POST /v1.0/me/sendMail call.
type Mail struct {
	ID          string          `json:"id"`
	ReceivedAt  time.Time       `json:"receivedAt"`
	From        string          `json:"from"`
	To          []string        `json:"to"`
	Subject     string          `json:"subject"`
	BodyType    string          `json:"bodyType"`
	BodyContent string          `json:"bodyContent"`
	ReplyTo     []string        `json:"replyTo,omitempty"`
	MessageID   string          `json:"messageId,omitempty"`
	RawRequest  json.RawMessage `json:"rawRequest"`
}

// Meeting captures one POST /v1.0/me/onlineMeetings call.
type Meeting struct {
	ID            string          `json:"id"`
	CreatedAt     time.Time       `json:"createdAt"`
	Subject       string          `json:"subject"`
	StartDateTime string          `json:"startDateTime"`
	EndDateTime   string          `json:"endDateTime"`
	JoinWebURL    string          `json:"joinWebUrl"`
	RawRequest   json.RawMessage `json:"rawRequest"`
}

// Store keeps the most recent N captures in memory. Oldest items are
// discarded when the buffer fills.
type Store struct {
	mu       sync.RWMutex
	mails    []*Mail
	meetings []*Meeting
	max      int
}

// New constructs a Store that retains up to max items per kind.
// max <= 0 defaults to 500.
func New(max int) *Store {
	if max <= 0 {
		max = 500
	}
	return &Store{max: max}
}

// AddMail records a captured sendMail call. Returns the assigned ID.
func (s *Store) AddMail(m *Mail) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == "" {
		m.ID = newID()
	}
	if m.ReceivedAt.IsZero() {
		m.ReceivedAt = time.Now().UTC()
	}
	s.mails = append([]*Mail{m}, s.mails...)
	if len(s.mails) > s.max {
		s.mails = s.mails[:s.max]
	}
	return m.ID
}

// AddMeeting records a captured onlineMeetings call. Returns the assigned ID.
func (s *Store) AddMeeting(m *Meeting) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == "" {
		m.ID = newID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	s.meetings = append([]*Meeting{m}, s.meetings...)
	if len(s.meetings) > s.max {
		s.meetings = s.meetings[:s.max]
	}
	return m.ID
}

// Mails returns a snapshot copy of captured mails (newest first).
func (s *Store) Mails() []*Mail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Mail, len(s.mails))
	copy(out, s.mails)
	return out
}

// MailByID returns the mail with the given ID, or nil if not found.
func (s *Store) MailByID(id string) *Mail {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.mails {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// ClearMails drops all captured mails.
func (s *Store) ClearMails() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mails = nil
}

// Meetings returns a snapshot copy of captured meetings (newest first).
func (s *Store) Meetings() []*Meeting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Meeting, len(s.meetings))
	copy(out, s.meetings)
	return out
}

// MeetingByID returns the meeting with the given ID, or nil if not found.
func (s *Store) MeetingByID(id string) *Meeting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.meetings {
		if m.ID == id {
			return m
		}
	}
	return nil
}

// ClearMeetings drops all captured meetings.
func (s *Store) ClearMeetings() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meetings = nil
}

// Counts returns the current number of captured mails and meetings.
func (s *Store) Counts() (mails, meetings int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.mails), len(s.meetings)
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
