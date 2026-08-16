package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const scheduleBody = `{
	"schedules": ["user@example.com"],
	"startTime": {"dateTime": "2026-08-20T09:00:00", "timeZone": "UTC"},
	"endTime":   {"dateTime": "2026-08-20T17:00:00", "timeZone": "UTC"},
	"availabilityViewInterval": 60
}`

func postSchedule(t *testing.T, url, body string) (*http.Response, scheduleResponse) {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	var decoded scheduleResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return resp, decoded
}

func TestGetScheduleReturnsFreeCalendar(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, decoded := postSchedule(t, srv.URL+"/v1.0/me/calendar/getSchedule", scheduleBody)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(decoded.Value) != 1 {
		t.Fatalf("value length = %d, want 1", len(decoded.Value))
	}
	if decoded.Value[0].ScheduleID != "user@example.com" {
		t.Errorf("scheduleId = %q, want the address that was asked about", decoded.Value[0].ScheduleID)
	}
	if len(decoded.Value[0].ScheduleItems) != 0 {
		t.Errorf("scheduleItems = %d, want none so every slot reads as free", len(decoded.Value[0].ScheduleItems))
	}
}

// the app-level path carries a user id, which a fixed route cannot match
func TestGetScheduleServesTheUsersPathToo(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, decoded := postSchedule(t, srv.URL+"/v1.0/users/u-1/calendar/getSchedule", scheduleBody)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(decoded.Value) != 1 {
		t.Fatalf("value length = %d, want 1", len(decoded.Value))
	}
}

func TestGetScheduleAnswersEverySchedule(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{
		"schedules": ["one@example.com", "two@example.com"],
		"startTime": {"dateTime": "2026-08-20T09:00:00", "timeZone": "UTC"},
		"endTime":   {"dateTime": "2026-08-20T10:00:00", "timeZone": "UTC"}
	}`
	_, decoded := postSchedule(t, srv.URL+"/v1.0/me/calendar/getSchedule", body)

	if len(decoded.Value) != 2 {
		t.Fatalf("value length = %d, want one entry per schedule", len(decoded.Value))
	}
	if decoded.Value[1].ScheduleID != "two@example.com" {
		t.Errorf("second scheduleId = %q, want the second address", decoded.Value[1].ScheduleID)
	}
}

func TestGetScheduleSizesTheAvailabilityView(t *testing.T) {
	srv, _ := newTestServer(t)

	// eight hours at one hour per interval
	_, decoded := postSchedule(t, srv.URL+"/v1.0/me/calendar/getSchedule", scheduleBody)

	if got := decoded.Value[0].AvailabilityView; got != "00000000" {
		t.Errorf("availabilityView = %q, want eight free hours", got)
	}
}

func TestGetScheduleRejectsNoSchedules(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"startTime": {"dateTime": "2026-08-20T09:00:00"}, "endTime": {"dateTime": "2026-08-20T10:00:00"}}`
	resp, _ := postSchedule(t, srv.URL+"/v1.0/me/calendar/getSchedule", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetScheduleRejectsGet(t *testing.T) {
	srv, _ := newTestServer(t)

	resp, err := http.Get(srv.URL + "/v1.0/me/calendar/getSchedule")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestAvailabilityView(t *testing.T) {
	cases := []struct {
		name     string
		start    string
		end      string
		interval int
		want     string
	}{
		{"defaults to half-hour intervals", "2026-08-20T09:00:00", "2026-08-20T11:00:00", 0, "0000"},
		{"honours the requested interval", "2026-08-20T09:00:00", "2026-08-20T11:00:00", 60, "00"},
		{"is empty when the window is inverted", "2026-08-20T11:00:00", "2026-08-20T09:00:00", 60, ""},
		{"is empty when the window is a point", "2026-08-20T09:00:00", "2026-08-20T09:00:00", 60, ""},
		{"rounds a partial interval down", "2026-08-20T09:00:00", "2026-08-20T09:59:00", 60, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, ok := parseGraphDateTime(tc.start)
			if !ok {
				t.Fatalf("could not parse %q", tc.start)
			}
			end, ok := parseGraphDateTime(tc.end)
			if !ok {
				t.Fatalf("could not parse %q", tc.end)
			}
			if got := availabilityView(start, end, tc.interval); got != tc.want {
				t.Errorf("availabilityView = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAvailabilityViewIsCapped(t *testing.T) {
	start, _ := parseGraphDateTime("2026-01-01T00:00:00")
	end, _ := parseGraphDateTime("2030-01-01T00:00:00")

	if got := len(availabilityView(start, end, 1)); got != maxAvailabilitySlots {
		t.Errorf("length = %d, want it capped at %d", got, maxAvailabilitySlots)
	}
}

func TestParseGraphDateTime(t *testing.T) {
	for _, value := range []string{
		"2026-08-20T09:00:00",
		"2026-08-20T09:00:00.0000000",
		"2026-08-20T09:00:00Z",
	} {
		if _, ok := parseGraphDateTime(value); !ok {
			t.Errorf("could not parse %q", value)
		}
	}

	for _, value := range []string{"", "not a date", "20/08/2026"} {
		if _, ok := parseGraphDateTime(value); ok {
			t.Errorf("parsed %q, want it rejected", value)
		}
	}
}
