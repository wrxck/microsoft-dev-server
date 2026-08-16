// Package server implements the HTTP handlers for the fake Microsoft Graph
// API and the dev-only inspection endpoints under /_dev/.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wrxck/microsoft-dev-server/internal/scenarios"
	"github.com/wrxck/microsoft-dev-server/internal/store"
)

// Config holds runtime configuration.
type Config struct {
	// User identity returned from GET /v1.0/me.
	UserEmail string
	UserName  string
	UserID    string
}

// handler bundles the http routing for the fake graph + inspection api.
type Handler struct {
	cfg       Config
	store     *store.Store
	scenarios *scenarios.Store
}

// new constructs a handler. scenarios may be nil for callers that don't
// want the forced-response feature.
func New(cfg Config, st *store.Store, sc *scenarios.Store) *Handler {
	if cfg.UserEmail == "" {
		cfg.UserEmail = "rebecca@dev.local"
	}
	if cfg.UserName == "" {
		cfg.UserName = "Rebecca Dev"
	}
	if cfg.UserID == "" {
		cfg.UserID = "00000000-0000-0000-0000-000000000001"
	}
	return &Handler{cfg: cfg, store: st, scenarios: sc}
}

// maybeForceScenario writes a queued forced response if one matches the
// current request. returns true if it served the response, in which
// case the caller must return without doing anything else.
func (h *Handler) maybeForceScenario(w http.ResponseWriter, r *http.Request) bool {
	if h.scenarios == nil {
		return false
	}
	sc := h.scenarios.MatchAndConsume(r.Method, r.URL.Path)
	if sc == nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Forced-Scenario", sc.ID)
	for k, v := range sc.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(sc.Status)
	_, _ = w.Write([]byte(sc.BodyJSON))
	return true
}

// Routes registers all routes on the given mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	// Graph API surface (only what we use today).
	mux.HandleFunc("/v1.0/me/calendar/getSchedule", h.getSchedule)
	mux.HandleFunc("/v1.0/me/sendMail", h.sendMail)
	mux.HandleFunc("/v1.0/me/onlineMeetings", h.onlineMeetings)
	mux.HandleFunc("/v1.0/me", h.me)

	// OAuth token endpoints (catch any tenant).
	mux.HandleFunc("/", h.dispatchRoot)

	// dev/inspection endpoints.
	mux.HandleFunc("/_dev/mail", h.devMailList)
	mux.HandleFunc("/_dev/mail/", h.devMailGet)
	mux.HandleFunc("/_dev/meetings", h.devMeetingsList)
	mux.HandleFunc("/_dev/meetings/", h.devMeetingsGet)
	mux.HandleFunc("/_dev/status", h.devStatus)
	mux.HandleFunc("/_dev/scenarios", h.devScenarios)
	mux.HandleFunc("/_dev/scenarios/", h.devScenarioDelete)
	mux.HandleFunc("/_dev/scenario-presets", h.devScenarioPresets)
}

// dispatchRoot routes the OAuth token endpoint and the UI root.
// Microsoft uses paths like /<tenant>/oauth2/v2.0/token or
// /common/oauth2/v2.0/token, so we match suffix.
func (h *Handler) dispatchRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
		h.token(w, r)
		return
	}
	// the app-level form carries a user id in the path, so it cannot be a
	// fixed route: /v1.0/users/{id}/calendar/getSchedule
	if strings.HasSuffix(r.URL.Path, "/calendar/getSchedule") {
		h.getSchedule(w, r)
		return
	}
	if r.URL.Path == "/" {
		h.uiIndex(w, r)
		return
	}
	http.NotFound(w, r)
}

// uiIndex is overridden by the main package once the embedded UI is wired up.
// The default 200 response just confirms the server is alive.
var uiHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html><meta charset=utf-8><title>microsoft-dev-server</title><h1>microsoft-dev-server</h1><p>Healthy. Use the inspection endpoints under <code>/_dev/</code>.</p>")
})

// SetUI lets the cmd binary install the embedded dark-mode UI handler.
func SetUI(h http.Handler) {
	uiHandler = h
}

func (h *Handler) uiIndex(w http.ResponseWriter, r *http.Request) {
	uiHandler.ServeHTTP(w, r)
}

// ----------------------------------------------------------------------------
// Graph API endpoints
// ----------------------------------------------------------------------------

// sendMail accepts the standard Graph sendMail JSON shape and stores the
// captured payload. Returns 202 Accepted with empty body, mirroring Graph.
func (h *Handler) sendMail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.maybeForceScenario(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req struct {
		Message struct {
			Subject string `json:"subject"`
			Body    struct {
				ContentType string `json:"contentType"`
				Content     string `json:"content"`
			} `json:"body"`
			ToRecipients []struct {
				EmailAddress struct {
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"toRecipients"`
			From *struct {
				EmailAddress struct {
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"from"`
			ReplyTo []struct {
				EmailAddress struct {
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"replyTo"`
			InternetMessageID string `json:"internetMessageId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	to := make([]string, 0, len(req.Message.ToRecipients))
	for _, t := range req.Message.ToRecipients {
		if t.EmailAddress.Address != "" {
			to = append(to, t.EmailAddress.Address)
		}
	}
	replyTo := make([]string, 0, len(req.Message.ReplyTo))
	for _, t := range req.Message.ReplyTo {
		if t.EmailAddress.Address != "" {
			replyTo = append(replyTo, t.EmailAddress.Address)
		}
	}
	from := h.cfg.UserEmail
	if req.Message.From != nil {
		from = req.Message.From.EmailAddress.Address
	}

	h.store.AddMail(&store.Mail{
		From:        from,
		To:          to,
		Subject:     req.Message.Subject,
		BodyType:    req.Message.Body.ContentType,
		BodyContent: req.Message.Body.Content,
		ReplyTo:     replyTo,
		MessageID:   req.Message.InternetMessageID,
		RawRequest:  body,
	})

	w.WriteHeader(http.StatusAccepted)
}

// onlineMeetings accepts a Teams meeting create request and returns the
// standard onlineMeeting response shape with a fake joinWebUrl.
func (h *Handler) onlineMeetings(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Subject       string `json:"subject"`
		StartDateTime string `json:"startDateTime"`
		EndDateTime   string `json:"endDateTime"`
	}
	_ = json.Unmarshal(body, &req)

	id := newRandomID()
	joinURL := fmt.Sprintf("https://teams.microsoft.com/l/meetup-join/dev_%s", id)

	h.store.AddMeeting(&store.Meeting{
		ID:            id,
		Subject:       req.Subject,
		StartDateTime: req.StartDateTime,
		EndDateTime:   req.EndDateTime,
		JoinWebURL:    joinURL,
		RawRequest:    body,
	})

	resp := map[string]any{
		"id":               id,
		"creationDateTime": time.Now().UTC().Format(time.RFC3339),
		"startDateTime":    req.StartDateTime,
		"endDateTime":      req.EndDateTime,
		"subject":          req.Subject,
		"joinWebUrl":       joinURL,
	}
	writeJSON(w, http.StatusCreated, resp)
}

// me returns a canned identity for the authenticated user.
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	if h.maybeForceScenario(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":                h.cfg.UserID,
		"displayName":       h.cfg.UserName,
		"mail":              h.cfg.UserEmail,
		"userPrincipalName": h.cfg.UserEmail,
	})
}

// token returns a fake OAuth bearer token. Accepts any grant type.
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "dev_" + newRandomID(),
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        "Mail.Send OnlineMeetings.ReadWrite User.Read",
	})
}

// ----------------------------------------------------------------------------
// Dev / inspection endpoints
// ----------------------------------------------------------------------------

func (h *Handler) devMailList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.store.Mails())
	case http.MethodDelete:
		h.store.ClearMails()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) devMailGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/_dev/mail/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	m := h.store.MailByID(id)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *Handler) devMeetingsList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.store.Meetings())
	case http.MethodDelete:
		h.store.ClearMeetings()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) devMeetingsGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/_dev/meetings/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	m := h.store.MeetingByID(id)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *Handler) devStatus(w http.ResponseWriter, r *http.Request) {
	mails, meetings := h.store.Counts()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"capturedMail":  mails,
		"capturedMeets": meetings,
		"now":           time.Now().UTC().Format(time.RFC3339),
		"user": map[string]string{
			"id":    h.cfg.UserID,
			"name":  h.cfg.UserName,
			"email": h.cfg.UserEmail,
		},
	})
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		// Body already partly written; nothing useful to do.
		_ = err
	}
}

func newRandomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// devScenarios handles GET (list) / POST (queue) / DELETE (clear all).
func (h *Handler) devScenarios(w http.ResponseWriter, r *http.Request) {
	if h.scenarios == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.scenarios.All())
	case http.MethodPost:
		var body struct {
			PresetKey    string            `json:"presetKey"`
			Description  string            `json:"description"`
			Method       string            `json:"method"`
			PathContains string            `json:"pathContains"`
			Status       int               `json:"status"`
			Headers      map[string]string `json:"headers"`
			BodyJSON     string            `json:"bodyJson"`
			OneShot      *bool             `json:"oneShot"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.PresetKey != "" {
			p := scenarios.PresetByKey(body.PresetKey)
			if p == nil {
				http.Error(w, "unknown preset key", http.StatusBadRequest)
				return
			}
			if body.Method == "" {
				body.Method = p.Method
			}
			if body.PathContains == "" {
				body.PathContains = p.PathContains
			}
			if body.Status == 0 {
				body.Status = p.Status
			}
			if body.BodyJSON == "" {
				body.BodyJSON = p.Body
			}
			if body.Description == "" {
				body.Description = p.Label
			}
			if body.Headers == nil {
				body.Headers = p.Headers
			}
		}
		if body.Status == 0 {
			http.Error(w, "status is required (or pick a preset)", http.StatusBadRequest)
			return
		}
		oneShot := true
		if body.OneShot != nil {
			oneShot = *body.OneShot
		}
		id := h.scenarios.Add(&scenarios.Scenario{
			Description:  body.Description,
			Method:       body.Method,
			PathContains: body.PathContains,
			Status:       body.Status,
			Headers:      body.Headers,
			BodyJSON:     body.BodyJSON,
			OneShot:      oneShot,
		})
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	case http.MethodDelete:
		h.scenarios.Clear()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// devScenarioDelete handles DELETE /_dev/scenarios/{id}.
func (h *Handler) devScenarioDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.scenarios == nil {
		http.Error(w, "scenarios disabled", http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/_dev/scenarios/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if !h.scenarios.Delete(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// devScenarioPresets returns the catalogue of named graph failures.
func (h *Handler) devScenarioPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, scenarios.Presets)
}
