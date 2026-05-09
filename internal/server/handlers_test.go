package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wrxck/microsoft-dev-server/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st := store.New(0)
	h := New(Config{
		UserEmail: "rebecca@x.com",
		UserName:  "Rebecca",
		UserID:    "u-1",
	}, st)
	mux := http.NewServeMux()
	h.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func TestSendMailCapturesPayload(t *testing.T) {
	srv, st := newTestServer(t)

	body := `{
		"message": {
			"subject": "Hello",
			"body": {"contentType": "HTML", "content": "<p>hi</p>"},
			"toRecipients": [{"emailAddress": {"address": "alice@x.com"}}],
			"replyTo":      [{"emailAddress": {"address": "noreply@x.com"}}],
			"internetMessageId": "<abc@x>"
		}
	}`
	resp, err := http.Post(srv.URL+"/v1.0/me/sendMail", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	mails := st.Mails()
	if len(mails) != 1 {
		t.Fatalf("want 1 captured mail, got %d", len(mails))
	}
	m := mails[0]
	if m.Subject != "Hello" {
		t.Errorf("subject = %q", m.Subject)
	}
	if len(m.To) != 1 || m.To[0] != "alice@x.com" {
		t.Errorf("to = %v", m.To)
	}
	if len(m.ReplyTo) != 1 || m.ReplyTo[0] != "noreply@x.com" {
		t.Errorf("replyTo = %v", m.ReplyTo)
	}
	if m.MessageID != "<abc@x>" {
		t.Errorf("messageID = %q", m.MessageID)
	}
	if m.BodyType != "HTML" || !strings.Contains(m.BodyContent, "<p>hi</p>") {
		t.Errorf("body mismatch: %q / %q", m.BodyType, m.BodyContent)
	}
}

func TestSendMailRejectsGet(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/v1.0/me/sendMail")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestSendMailInvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Post(srv.URL+"/v1.0/me/sendMail", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestOnlineMeetingsReturnsJoinURL(t *testing.T) {
	srv, st := newTestServer(t)
	body := `{"subject":"Algebra","startDateTime":"2026-06-09T17:30:00Z","endDateTime":"2026-06-09T17:45:00Z"}`
	resp, err := http.Post(srv.URL+"/v1.0/me/onlineMeetings", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got struct {
		ID         string `json:"id"`
		JoinWebURL string `json:"joinWebUrl"`
		Subject    string `json:"subject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Errorf("missing id")
	}
	if !strings.HasPrefix(got.JoinWebURL, "https://teams.microsoft.com/l/meetup-join/dev_") {
		t.Errorf("joinWebUrl = %q", got.JoinWebURL)
	}
	if got.Subject != "Algebra" {
		t.Errorf("subject = %q", got.Subject)
	}
	if len(st.Meetings()) != 1 {
		t.Errorf("expected 1 captured meeting")
	}
}

func TestMeReturnsCannedIdentity(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/v1.0/me")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["mail"] != "rebecca@x.com" {
		t.Errorf("mail = %q", got["mail"])
	}
	if got["displayName"] != "Rebecca" {
		t.Errorf("displayName = %q", got["displayName"])
	}
}

func TestTokenEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Post(
		srv.URL+"/common/oauth2/v2.0/token",
		"application/x-www-form-urlencoded",
		strings.NewReader("grant_type=client_credentials&client_id=x&client_secret=y"),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tok, _ := got["access_token"].(string); !strings.HasPrefix(tok, "dev_") {
		t.Errorf("access_token = %v", got["access_token"])
	}
	if got["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", got["token_type"])
	}
}

func TestDevMailListAndClear(t *testing.T) {
	srv, st := newTestServer(t)
	st.AddMail(&store.Mail{Subject: "x"})

	resp, err := http.Get(srv.URL + "/_dev/mail")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/_dev/mail", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp2.StatusCode)
	}
	if len(st.Mails()) != 0 {
		t.Errorf("expected store cleared")
	}
}

func TestDevMailGetByID(t *testing.T) {
	srv, st := newTestServer(t)
	id := st.AddMail(&store.Mail{Subject: "find-me"})

	resp, err := http.Get(srv.URL + "/_dev/mail/" + id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	resp2, err := http.Get(srv.URL + "/_dev/mail/missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("missing id status = %d, want 404", resp2.StatusCode)
	}
}

func TestDevStatus(t *testing.T) {
	srv, st := newTestServer(t)
	st.AddMail(&store.Mail{})
	st.AddMeeting(&store.Meeting{})

	resp, err := http.Get(srv.URL + "/_dev/status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["capturedMail"].(float64) != 1 || got["capturedMeets"].(float64) != 1 {
		t.Errorf("counts wrong: %v", got)
	}
}

// guard against unused-import drift
var _ = bytes.NewBuffer
