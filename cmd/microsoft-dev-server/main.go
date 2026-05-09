// microsoft-dev-server is a fake Microsoft Graph API server for local
// development and testing. It captures sendMail and onlineMeeting create
// calls, exposes a dark-mode inspection UI, and ships an MCP server so an
// LLM client can query captured traffic.
//
// Run modes:
//
//	microsoft-dev-server                   # default: HTTP server + UI
//	microsoft-dev-server mcp [--upstream]  # stdio MCP server
//	microsoft-dev-server version
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wrxck/microsoft-dev-server/internal/mcp"
	"github.com/wrxck/microsoft-dev-server/internal/server"
	"github.com/wrxck/microsoft-dev-server/internal/store"
	"github.com/wrxck/microsoft-dev-server/internal/ui"
)

var version = "dev"

const banner = `microsoft-dev-server %s
A fake Microsoft Graph API for development and testing.
HTTP API + Web UI: http://%s
`

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "mcp":
			os.Exit(runMCP(os.Args[2:]))
		case "version", "--version", "-v":
			fmt.Printf("microsoft-dev-server %s\n", version)
			return
		case "help", "--help", "-h":
			usage()
			return
		}
	}
	os.Exit(runHTTP(os.Args[1:]))
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage:
  microsoft-dev-server                  Run HTTP server + Web UI
  microsoft-dev-server mcp [--upstream] Run MCP server over stdio
  microsoft-dev-server version          Show version
`)
}

func runHTTP(args []string) int {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	addr := fs.String("addr", envOr("ADDR", "127.0.0.1:8080"), "HTTP listen address")
	maxItems := fs.Int("max-items", 500, "Max captured items per kind")
	userEmail := fs.String("user-email", envOr("DEV_USER_EMAIL", "rebecca@dev.local"), "Identity returned from /v1.0/me")
	userName := fs.String("user-name", envOr("DEV_USER_NAME", "Rebecca Dev"), "Display name returned from /v1.0/me")
	userID := fs.String("user-id", envOr("DEV_USER_ID", "00000000-0000-0000-0000-000000000001"), "User id returned from /v1.0/me")
	_ = fs.Parse(args)

	st := store.New(*maxItems)
	server.SetUI(ui.Handler())
	h := server.New(server.Config{
		UserEmail: *userEmail,
		UserName:  *userName,
		UserID:    *userID,
	}, st)
	mux := http.NewServeMux()
	h.Routes(mux)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           logging(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Printf(banner, version, *addr)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "http error:", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
		return 0
	}
}

func runMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	upstream := fs.String("upstream", envOr("MCP_UPSTREAM", "http://127.0.0.1:8080"), "Base URL of a running microsoft-dev-server")
	_ = fs.Parse(args)
	if err := mcp.Run(context.Background(), os.Stdin, os.Stdout, *upstream); err != nil {
		fmt.Fprintln(os.Stderr, "mcp error:", err)
		return 1
	}
	return 0
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		fmt.Fprintf(os.Stdout, "%s %s %s -> %d (%s)\n",
			start.Format(time.RFC3339), r.Method, r.URL.Path, ww.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
