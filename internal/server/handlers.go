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

	"github.com/wrxck/microsoft-dev-server/internal/store"
)

// Config holds runtime configuration.
type Config struct {
	// User identity returned from GET /v1.0/me.
	UserEmail string
	UserName  string
	UserID    string
}

// Handler bundles the HTTP routing for the fake Graph + inspection API.
type Handler struct {
	cfg   Config
	store *store.Store
}

// New constructs a Handler.
func New(cfg Config, st *store.Store) *Handler {
	if cfg.UserEmail == "" {
		cfg.UserEmail = "rebecca@dev.local"
	}
	if cfg.UserName == "" {
		cfg.UserName = "Rebecca Dev"
	}
	if cfg.UserID == "" {
		cfg.UserID = "00000000-0000-0000-0000-000000000001"
	}
	return &Handler{cfg: cfg, store: st}
}

// Routes registers all routes on the given mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	// Graph API surface (only what we use today).
	mux.HandleFunc("/v1.0/me/sendMail", h.sendMail)
	mux.HandleFunc("/v1.0/me/onlineMeetings", h.onlineMeetings)
	mux.HandleFunc("/v1.0/me", h.me)

	// OAuth token endpoints (catch any tenant).
	mux.HandleFunc("/", h.dispatchRoot)

	// Dev/inspection endpoints.
	mux.HandleFunc("/_dev/mail", h.devMailList)
	mux.HandleFunc("/_dev/mail/", h.devMailGet)
	mux.HandleFunc("/_dev/meetings", h.devMeetingsList)
	mux.HandleFunc("/_dev/meetings/", h.devMeetingsGet)
	mux.HandleFunc("/_dev/status", h.devStatus)
}

// dispatchRoot routes the OAuth token endpoint and the UI root.
// Microsoft uses paths like /<tenant>/oauth2/v2.0/token or
// /common/oauth2/v2.0/token, so we match suffix.
func (h *Handler) dispatchRoot(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
		h.token(w, r)
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
