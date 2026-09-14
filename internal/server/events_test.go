package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const eventBody = `{
	"subject": "Assessment - Tom Smith",
	"body": {"contentType": "HTML", "content": "<p>Assessment</p>"},
	"start": {"dateTime": "2026-07-01T10:00:00", "timeZone": "Europe/London"},
	"end":   {"dateTime": "2026-07-01T11:00:00", "timeZone": "Europe/London"},
	"location": {"displayName": "12 Example Road, London, SE21 7AA"},
	"attendees": [{"emailAddress": {"address": "parent@x.com"}}]
}`

func TestEventsCapturesPayload(t *testing.T) {
	srv, st := newTestServer(t)

	res, err := http.Post(srv.URL+"/v1.0/me/events", "application/json", strings.NewReader(eventBody))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}

	var resp struct {
		ID       string `json:"id"`
		WebLink  string `json:"webLink"`
		Subject  string `json:"subject"`
		Location struct {
			DisplayName string `json:"displayName"`
		} `json:"location"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" || !strings.HasPrefix(resp.WebLink, "https://outlook.office365.com/") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Location.DisplayName != "12 Example Road, London, SE21 7AA" {
		t.Fatalf("location = %q, want the address echoed back", resp.Location.DisplayName)
	}

	events := st.Events()
	if len(events) != 1 {
		t.Fatalf("captured %d events, want 1", len(events))
	}
	e := events[0]
	if e.Subject != "Assessment - Tom Smith" {
		t.Errorf("subject = %q", e.Subject)
	}
	if e.Location != "12 Example Road, London, SE21 7AA" {
		t.Errorf("location = %q", e.Location)
	}
	if e.StartDateTime != "2026-07-01T10:00:00" || e.EndDateTime != "2026-07-01T11:00:00" {
		t.Errorf("times = %q..%q", e.StartDateTime, e.EndDateTime)
	}
	if e.TimeZone != "Europe/London" {
		t.Errorf("timeZone = %q", e.TimeZone)
	}
	if len(e.Attendees) != 1 || e.Attendees[0] != "parent@x.com" {
		t.Errorf("attendees = %v", e.Attendees)
	}
	if e.BodyContent != "<p>Assessment</p>" {
		t.Errorf("bodyContent = %q", e.BodyContent)
	}
}

func TestEventsRejectsGet(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Get(srv.URL + "/v1.0/me/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", res.StatusCode)
	}
}

func TestEventsInvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Post(srv.URL+"/v1.0/me/events", "application/json", strings.NewReader("{nope"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
}

// The app-level (client-credentials) form names the mailbox in the path,
// which is what a real Graph client must send. Every one of these 404'd
// before the dispatcher understood the user-scoped form.
func TestUserScopedPathsReachTheSameHandlers(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"events", "/v1.0/users/abc@x.com/events", eventBody, http.StatusCreated},
		{"calendar events", "/v1.0/users/abc@x.com/calendar/events", eventBody, http.StatusCreated},
		{"online meetings", "/v1.0/users/abc@x.com/onlineMeetings", `{"subject":"m"}`, http.StatusCreated},
		{
			"send mail",
			"/v1.0/users/abc@x.com/sendMail",
			`{"message":{"subject":"s","toRecipients":[{"emailAddress":{"address":"a@x.com"}}]}}`,
			http.StatusAccepted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			res, err := http.Post(srv.URL+tc.path, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
}

func TestUserScopedMailboxLookup(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Get(srv.URL + "/v1.0/users/abc@x.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// One mailbox is stood in for, whatever id was asked for.
	if body["mail"] != "rebecca@x.com" {
		t.Fatalf("mail = %q", body["mail"])
	}
}

// A path that is not a mailbox action must still 404 rather than be swallowed
// by the suffix matching.
func TestUnknownPathsStillNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	for _, p := range []string{"/v1.0/users", "/v1.0/groups/abc/events", "/v1.0/me/nonsense"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", p, res.StatusCode)
		}
	}
}

func TestDevEventsListGetAndClear(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Post(srv.URL+"/v1.0/me/events", "application/json", strings.NewReader(eventBody))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	listRes, err := http.Get(srv.URL + "/_dev/events")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listRes.Body.Close()
	var list []struct {
		ID      string `json:"id"`
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d events, want 1", len(list))
	}

	oneRes, err := http.Get(srv.URL + "/_dev/events/" + list[0].ID)
	if err != nil {
		t.Fatalf("get one: %v", err)
	}
	oneRes.Body.Close()
	if oneRes.StatusCode != http.StatusOK {
		t.Fatalf("get one status = %d", oneRes.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/_dev/events", nil)
	delRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	delRes.Body.Close()

	afterRes, err := http.Get(srv.URL + "/_dev/events")
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	defer afterRes.Body.Close()
	var after []any
	if err := json.NewDecoder(afterRes.Body).Decode(&after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after clear = %d events, want 0", len(after))
	}
}

func TestDevStatusCountsEvents(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Post(srv.URL+"/v1.0/me/events", "application/json", strings.NewReader(eventBody))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	statusRes, err := http.Get(srv.URL + "/_dev/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	defer statusRes.Body.Close()
	var body struct {
		CapturedEvents int `json:"capturedEvents"`
	}
	if err := json.NewDecoder(statusRes.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.CapturedEvents != 1 {
		t.Fatalf("capturedEvents = %d, want 1", body.CapturedEvents)
	}
}
