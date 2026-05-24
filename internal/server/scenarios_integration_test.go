package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wrxck/microsoft-dev-server/internal/scenarios"
	"github.com/wrxck/microsoft-dev-server/internal/store"
)

// when a scenario is queued the matching graph handler must return the
// forced response instead of the normal mock behaviour. proves a
// consumer's error-decode path is exercised with the same body shape
// graph's real api would emit.
func TestSendMail_ForcedScenarioShortCircuits(t *testing.T) {
	st := store.New(0)
	sc := scenarios.NewStore()
	preset := scenarios.PresetByKey("throttled")
	if preset == nil {
		t.Fatal("expected throttled preset")
	}
	sc.Add(&scenarios.Scenario{
		Method:       preset.Method,
		PathContains: preset.PathContains,
		Status:       preset.Status,
		Headers:      preset.Headers,
		BodyJSON:     preset.Body,
		OneShot:      true,
	})

	h := New(Config{UserEmail: "x@y", UserName: "x", UserID: "1"}, st, sc)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1.0/me/sendMail", "application/json",
		bytes.NewReader([]byte(`{"message":{"subject":"x","toRecipients":[{"emailAddress":{"address":"a@b"}}],"body":{"contentType":"text","content":"y"}}}`)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 429 {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("X-Forced-Scenario") == "" {
		t.Errorf("expected X-Forced-Scenario header")
	}
	if ra := resp.Header.Get("Retry-After"); ra != "30" {
		t.Errorf("Retry-After = %q, want 30 (from preset headers)", ra)
	}

	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	errObj, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %s", body)
	}
	if errObj["code"] != "TooManyRequests" {
		t.Errorf("wrong code: %v", errObj["code"])
	}

	// store should NOT have a capture for the forced response — the
	// mock didn't actually process it as a real sendMail.
	mail := st.Mails()
	if len(mail) != 0 {
		t.Errorf("forced scenario should not be captured as real mail; got %d", len(mail))
	}

	// second call should fall through to normal mock behaviour now that
	// the one-shot scenario is consumed.
	resp2, _ := http.Post(srv.URL+"/v1.0/me/sendMail", "application/json",
		bytes.NewReader([]byte(`{"message":{"subject":"real","toRecipients":[{"emailAddress":{"address":"a@b"}}],"body":{"contentType":"text","content":"y"}}}`)))
	resp2.Body.Close()
	if resp2.StatusCode != 202 {
		t.Errorf("normal sendMail status = %d, want 202", resp2.StatusCode)
	}
	if len(st.Mails()) != 1 {
		t.Errorf("normal sendMail should have been captured")
	}
}

func TestOnlineMeetings_ForcedScenarioShortCircuits(t *testing.T) {
	st := store.New(0)
	sc := scenarios.NewStore()
	preset := scenarios.PresetByKey("online_meeting_conflict")
	sc.Add(&scenarios.Scenario{
		Method:       preset.Method,
		PathContains: preset.PathContains,
		Status:       preset.Status,
		BodyJSON:     preset.Body,
		OneShot:      true,
	})

	h := New(Config{UserEmail: "x@y"}, st, sc)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1.0/me/onlineMeetings", "application/json",
		bytes.NewReader([]byte(`{"subject":"x","startDateTime":"2026-01-01T00:00:00Z","endDateTime":"2026-01-01T01:00:00Z"}`)))
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestNilScenarioStoreSkipsCheck(t *testing.T) {
	st := store.New(0)
	h := New(Config{UserEmail: "x@y"}, st, nil)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/v1.0/me/sendMail", "application/json",
		bytes.NewReader([]byte(`{"message":{"subject":"x","toRecipients":[{"emailAddress":{"address":"a@b"}}],"body":{"contentType":"text","content":"y"}}}`)))
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Errorf("nil scenarios store should not break normal handler; got %d", resp.StatusCode)
	}
}
