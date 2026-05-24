package scenarios

import "fmt"

// preset captures a documented microsoft graph failure mode. bodies
// mirror the shape graph's real api returns — verified against
// learn.microsoft.com/graph/errors so any consumer's error-decode path
// is exercised exactly as in production.
type Preset struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Method       string `json:"method"`
	PathContains string `json:"pathContains"`
	Status       int    `json:"status"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string `json:"body"`
}

// presets is the catalogue.
var Presets = []Preset{
	{
		Key:          "auth_invalid_token",
		Label:        "401 InvalidAuthenticationToken",
		Description:  "401 returned by every graph endpoint when the bearer token is missing or invalid.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       401,
		Body:         graphError("InvalidAuthenticationToken", "Access token is empty."),
	},
	{
		Key:          "auth_token_expired",
		Label:        "401 InvalidAuthenticationToken (expired)",
		Description:  "401 with the token-expired signal a consumer's refresh flow should react to.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       401,
		Body:         graphError("InvalidAuthenticationToken", "CompactToken validation failed with reason code: 80049228."),
	},
	{
		Key:          "forbidden_access_denied",
		Label:        "403 AccessDenied",
		Description:  "403 when the app/user lacks the required scope.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       403,
		Body:         graphError("AccessDenied", "Insufficient privileges to complete the operation."),
	},
	{
		Key:          "sendmail_access_denied",
		Label:        "403 ErrorAccessDenied (sendMail)",
		Description:  "403 specific to sendMail — mailbox-level deny.",
		Method:       "POST",
		PathContains: "/sendMail",
		Status:       403,
		Body:         graphError("ErrorAccessDenied", "Access is denied. Check credentials and try again."),
	},
	{
		Key:          "sendmail_quota_exceeded",
		Label:        "507 ErrorQuotaExceeded (sendMail)",
		Description:  "507 when the sending mailbox exceeded its send quota.",
		Method:       "POST",
		PathContains: "/sendMail",
		Status:       507,
		Body:         graphError("ErrorQuotaExceeded", "The mailbox couldn't be created because the mailbox is full."),
	},
	{
		Key:          "throttled",
		Label:        "429 TooManyRequests (with Retry-After)",
		Description:  "429 with a Retry-After header — the canonical graph throttle response.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       429,
		Headers:      map[string]string{"Retry-After": "30"},
		Body:         graphError("TooManyRequests", "Too many requests. Please retry after the time specified in the Retry-After header."),
	},
	{
		Key:          "service_unavailable",
		Label:        "503 ServiceNotAvailable",
		Description:  "503 transient outage — consumer should retry with backoff.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       503,
		Headers:      map[string]string{"Retry-After": "60"},
		Body:         graphError("ServiceNotAvailable", "Service is temporarily unavailable."),
	},
	{
		Key:          "bad_request",
		Label:        "400 BadRequest",
		Description:  "400 generic validation error.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       400,
		Body:         graphError("BadRequest", "The request is invalid."),
	},
	{
		Key:          "not_found",
		Label:        "404 itemNotFound",
		Description:  "404 — a referenced resource (mailbox, calendar, meeting) does not exist.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       404,
		Body:         graphError("itemNotFound", "The specified object was not found in the store."),
	},
	{
		Key:          "internal_error",
		Label:        "500 InternalServerError",
		Description:  "500 generic graph-side outage.",
		Method:       "",
		PathContains: "/v1.0/",
		Status:       500,
		Body:         graphError("InternalServerError", "An internal server error occurred."),
	},
	{
		Key:          "online_meeting_conflict",
		Label:        "409 Conflict (onlineMeetings)",
		Description:  "409 when a meeting at that time already exists for the organiser.",
		Method:       "POST",
		PathContains: "/onlineMeetings",
		Status:       409,
		Body:         graphError("Conflict", "An online meeting with the specified parameters already exists."),
	},
}

// graphError builds a graph-shaped error envelope.
func graphError(code, message string) string {
	return fmt.Sprintf(`{"error":{"code":%q,"message":%q,"innerError":{"date":"2026-05-24T00:00:00","request-id":"dev-mock","client-request-id":"dev-mock"}}}`, code, message)
}

func PresetByKey(key string) *Preset {
	for i := range Presets {
		if Presets[i].Key == key {
			return &Presets[i]
		}
	}
	return nil
}
