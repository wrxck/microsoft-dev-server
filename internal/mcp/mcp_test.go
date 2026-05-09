package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newUpstream(t *testing.T) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/_dev/mail", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `[{"id":"m1","subject":"Hello"}]`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/_dev/mail/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"m1","subject":"Hello"}`)
	})
	mux.HandleFunc("/_dev/meetings", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	})
	mux.HandleFunc("/_dev/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true,"capturedMail":1,"capturedMeets":0}`)
	})
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

func runOnce(t *testing.T, upstream string, requests []string) []string {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	out := &threadSafeBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Run(ctx, in, out, upstream); err != nil && err != io.EOF && err != context.DeadlineExceeded {
		t.Fatalf("Run returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	return lines
}

func TestInitialize(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 response, got %d (%v)", len(out), out)
	}
	var resp rpcResponse
	if err := json.Unmarshal([]byte(out[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result wrong type: %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
}

func TestToolsList(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})
	var resp rpcResponse
	_ = json.Unmarshal([]byte(out[0]), &resp)
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) < 5 {
		t.Errorf("expected ≥5 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, t := range tools {
		names[t.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{
		"list_captured_mail", "get_captured_mail", "clear_captured_mail",
		"list_captured_meetings", "get_server_status",
	} {
		if !names[n] {
			t.Errorf("missing tool %q", n)
		}
	}
}

func TestToolsCallListMail(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_captured_mail","arguments":{}}}`,
	})
	var resp rpcResponse
	_ = json.Unmarshal([]byte(out[0]), &resp)
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)
	if !strings.Contains(content["text"].(string), `"subject": "Hello"`) {
		t.Errorf("text missing expected payload: %v", content["text"])
	}
}

func TestToolsCallGetMissingID(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_captured_mail","arguments":{}}}`,
	})
	var resp rpcResponse
	_ = json.Unmarshal([]byte(out[0]), &resp)
	result := resp.Result.(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true, got %v", result)
	}
}

func TestUnknownMethod(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{
		`{"jsonrpc":"2.0","id":5,"method":"do_a_barrel_roll"}`,
	})
	var resp rpcResponse
	_ = json.Unmarshal([]byte(out[0]), &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected -32601, got %+v", resp.Error)
	}
}

func TestInvalidJSONReturnsParseError(t *testing.T) {
	upstream, stop := newUpstream(t)
	defer stop()

	out := runOnce(t, upstream, []string{`{not-json`})
	var resp rpcResponse
	_ = json.Unmarshal([]byte(out[0]), &resp)
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Errorf("expected -32700, got %+v", resp.Error)
	}
}

// threadSafeBuffer is a tiny io.Writer that's safe for concurrent writes
// (Run holds a single goroutine but the encoder may call Write multiple
// times per Encode).
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
