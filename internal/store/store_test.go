package store

import (
	"sync"
	"testing"
)

func TestAddAndListMail(t *testing.T) {
	s := New(0)
	id1 := s.AddMail(&Mail{Subject: "first"})
	id2 := s.AddMail(&Mail{Subject: "second"})

	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("expected unique non-empty IDs, got %q, %q", id1, id2)
	}

	list := s.Mails()
	if len(list) != 2 {
		t.Fatalf("expected 2 mails, got %d", len(list))
	}
	if list[0].Subject != "second" {
		t.Fatalf("expected newest-first ordering, got first subject = %q", list[0].Subject)
	}

	if got := s.MailByID(id1); got == nil || got.Subject != "first" {
		t.Fatalf("MailByID(%q) returned %v, want subject=first", id1, got)
	}
	if got := s.MailByID("nope"); got != nil {
		t.Fatalf("MailByID for missing id should return nil, got %v", got)
	}
}

func TestMailRingBufferEvictsOldest(t *testing.T) {
	s := New(2)
	s.AddMail(&Mail{Subject: "a"})
	s.AddMail(&Mail{Subject: "b"})
	s.AddMail(&Mail{Subject: "c"})

	list := s.Mails()
	if len(list) != 2 {
		t.Fatalf("expected 2 (max), got %d", len(list))
	}
	if list[0].Subject != "c" || list[1].Subject != "b" {
		t.Fatalf("expected [c,b], got [%s,%s]", list[0].Subject, list[1].Subject)
	}
}

func TestClearMails(t *testing.T) {
	s := New(0)
	s.AddMail(&Mail{Subject: "x"})
	s.ClearMails()
	if len(s.Mails()) != 0 {
		t.Fatalf("expected empty after Clear, got %d", len(s.Mails()))
	}
}

func TestAddAndListMeeting(t *testing.T) {
	s := New(0)
	id := s.AddMeeting(&Meeting{Subject: "Algebra catch-up", JoinWebURL: "https://teams/abc"})
	if got := s.MeetingByID(id); got == nil || got.Subject != "Algebra catch-up" {
		t.Fatalf("MeetingByID returned %v", got)
	}
	if list := s.Meetings(); len(list) != 1 {
		t.Fatalf("expected 1 meeting, got %d", len(list))
	}
	s.ClearMeetings()
	if len(s.Meetings()) != 0 {
		t.Fatalf("expected empty after ClearMeetings")
	}
}

func TestCounts(t *testing.T) {
	s := New(0)
	s.AddMail(&Mail{})
	s.AddMail(&Mail{})
	s.AddMeeting(&Meeting{})
	mails, meetings := s.Counts()
	if mails != 2 || meetings != 1 {
		t.Fatalf("expected (2,1), got (%d,%d)", mails, meetings)
	}
}

func TestConcurrentAddIsSafe(t *testing.T) {
	s := New(0)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AddMail(&Mail{Subject: "x"})
			s.AddMeeting(&Meeting{Subject: "y"})
		}()
	}
	wg.Wait()
	mails, meetings := s.Counts()
	if mails != 20 || meetings != 20 {
		t.Fatalf("race resulted in counts (%d,%d) want (20,20)", mails, meetings)
	}
}
