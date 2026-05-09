// Package mcp provides a minimal stdio JSON-RPC 2.0 implementation of the
// Model Context Protocol so that an LLM client (e.g. Claude Code) can
// inspect captured Microsoft Graph traffic on a running microsoft-dev-server
// instance.
//
// The MCP process is stateless: it talks to the server's HTTP /_dev/* API
// over localhost. Start it with:
//
//	microsoft-dev-server mcp --upstream http://127.0.0.1:8080
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Run reads JSON-RPC requests from r and writes responses to w until r is
// closed or an unrecoverable error occurs. The upstream URL is the base of a
// running microsoft-dev-server HTTP server (e.g. "http://127.0.0.1:8080").
func Run(ctx context.Context, r io.Reader, w io.Writer, upstream string) error {
	if upstream == "" {
		upstream = "http://127.0.0.1:8080"
	}
	if _, err := url.Parse(upstream); err != nil {
		return fmt.Errorf("invalid upstream URL %q: %w", upstream, err)
	}
	upstream = strings.TrimRight(upstream, "/")

	server := &server{upstream: upstream, http: &http.Client{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	enc := json.NewEncoder(w)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}
		resp := server.handle(ctx, req)
		if req.ID == nil {
			// notification — no response
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// ----------------------------------------------------------------------------
// JSON-RPC envelopes
// ----------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ----------------------------------------------------------------------------
// Server
// ----------------------------------------------------------------------------

type server struct {
	upstream string
	http     *http.Client
}

func (s *server) handle(ctx context.Context, req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "microsoft-dev-server",
				"version": "0.1.0",
			},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": tools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
			return resp
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			resp.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			}
			return resp
		}
		text, _ := json.MarshalIndent(out, "", "  ")
		resp.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_captured_mail",
			"description": "Returns the list of POST /v1.0/me/sendMail calls captured by the running microsoft-dev-server. Newest first.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "get_captured_mail",
			"description": "Returns the full captured Mail object for the given id.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "clear_captured_mail",
			"description": "Drops all captured mails from the in-memory store.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "list_captured_meetings",
			"description": "Returns the list of POST /v1.0/me/onlineMeetings calls captured by the running microsoft-dev-server. Newest first.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "get_captured_meeting",
			"description": "Returns the full captured Meeting object for the given id.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
			},
		},
		{
			"name":        "clear_captured_meetings",
			"description": "Drops all captured meetings from the in-memory store.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "get_server_status",
			"description": "Returns the running server's status: counts and configured user identity.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func (s *server) callTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	switch name {
	case "list_captured_mail":
		return s.getJSON(ctx, "/_dev/mail")
	case "get_captured_mail":
		var a struct{ ID string `json:"id"` }
		_ = json.Unmarshal(args, &a)
		if a.ID == "" {
			return nil, fmt.Errorf("missing id")
		}
		return s.getJSON(ctx, "/_dev/mail/"+url.PathEscape(a.ID))
	case "clear_captured_mail":
		return s.deleteJSON(ctx, "/_dev/mail")
	case "list_captured_meetings":
		return s.getJSON(ctx, "/_dev/meetings")
	case "get_captured_meeting":
		var a struct{ ID string `json:"id"` }
		_ = json.Unmarshal(args, &a)
		if a.ID == "" {
			return nil, fmt.Errorf("missing id")
		}
		return s.getJSON(ctx, "/_dev/meetings/"+url.PathEscape(a.ID))
	case "clear_captured_meetings":
		return s.deleteJSON(ctx, "/_dev/meetings")
	case "get_server_status":
		return s.getJSON(ctx, "/_dev/status")
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *server) getJSON(ctx context.Context, path string) (any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.upstream+path, nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(body))
	}
	var v any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *server) deleteJSON(ctx context.Context, path string) (any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, s.upstream+path, nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(body))
	}
	return map[string]any{"ok": true}, nil
}
