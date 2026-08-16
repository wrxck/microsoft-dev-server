package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// the calendar getSchedule action.
//
// graph exposes it at both /v1.0/me/calendar/getSchedule and
// /v1.0/users/{id}/calendar/getSchedule, and a client uses whichever suits the
// credentials it holds, so both paths route here.
//
// the default answer is a completely free calendar: no schedule items and an
// availability view of all zeroes. that is the useful default for a
// development server, because it lets a booking flow offer every slot in the
// window it asked about. a busy calendar, an error, or any other response can
// be forced through the scenarios mechanism, like every other endpoint.
//
// https://learn.microsoft.com/graph/api/calendar-getschedule

// the shape graph uses for a local date-time plus its zone
type graphDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type scheduleRequest struct {
	Schedules                []string      `json:"schedules"`
	StartTime                graphDateTime `json:"startTime"`
	EndTime                  graphDateTime `json:"endTime"`
	AvailabilityViewInterval int           `json:"availabilityViewInterval"`
}

type scheduleItem struct {
	Status  string        `json:"status"`
	Subject string        `json:"subject,omitempty"`
	Start   graphDateTime `json:"start"`
	End     graphDateTime `json:"end"`
}

type workingHours struct {
	DaysOfWeek []string `json:"daysOfWeek"`
	StartTime  string   `json:"startTime"`
	EndTime    string   `json:"endTime"`
	TimeZone   struct {
		Name string `json:"name"`
	} `json:"timeZone"`
}

type scheduleInformation struct {
	ScheduleID       string         `json:"scheduleId"`
	AvailabilityView string         `json:"availabilityView"`
	ScheduleItems    []scheduleItem `json:"scheduleItems"`
	WorkingHours     workingHours   `json:"workingHours"`
}

type scheduleResponse struct {
	Value []scheduleInformation `json:"value"`
}

// what graph uses when a caller omits availabilityViewInterval
const defaultAvailabilityInterval = 30

// caps the generated availability view, so a request for a huge window with a
// tiny interval cannot make the server allocate without bound
const maxAvailabilitySlots = 10000

// accepts the forms graph emits: a plain local date-time, one carrying
// fractional seconds, or a full rfc 3339 instant
func parseGraphDateTime(value string) (time.Time, bool) {
	for _, layout := range []string{
		"2006-01-02T15:04:05.9999999",
		"2006-01-02T15:04:05",
		time.RFC3339,
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// one character per interval, "0" meaning free. the length is how a client
// sees that its window was understood, so it is derived from the request
// rather than fixed.
func availabilityView(start, end time.Time, intervalMinutes int) string {
	if intervalMinutes <= 0 {
		intervalMinutes = defaultAvailabilityInterval
	}
	if !end.After(start) {
		return ""
	}

	slots := int(end.Sub(start).Minutes()) / intervalMinutes
	if slots > maxAvailabilitySlots {
		slots = maxAvailabilitySlots
	}

	return strings.Repeat("0", slots)
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.maybeForceScenario(w, r) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req scheduleRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// graph rejects a request carrying no schedules, and a client that sends
	// none has a bug worth surfacing rather than papering over
	if len(req.Schedules) == 0 {
		http.Error(w, "the schedules parameter must contain at least one entry",
			http.StatusBadRequest)
		return
	}

	view := ""
	if start, ok := parseGraphDateTime(req.StartTime.DateTime); ok {
		if end, ok := parseGraphDateTime(req.EndTime.DateTime); ok {
			view = availabilityView(start, end, req.AvailabilityViewInterval)
		}
	}

	hours := workingHours{
		DaysOfWeek: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime:  "08:00:00.0000000",
		EndTime:    "17:00:00.0000000",
	}
	hours.TimeZone.Name = "UTC"

	out := scheduleResponse{Value: make([]scheduleInformation, 0, len(req.Schedules))}
	for _, id := range req.Schedules {
		out.Value = append(out.Value, scheduleInformation{
			ScheduleID:       id,
			AvailabilityView: view,
			// free by default; force a busy calendar with a scenario
			ScheduleItems: []scheduleItem{},
			WorkingHours:  hours,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
