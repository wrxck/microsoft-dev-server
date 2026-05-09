# microsoft-dev-server

A fake Microsoft Graph API server for development and testing. Captures the
Graph calls your application makes (sendMail, online-meeting create) and lets
you inspect them in a dark-mode web UI or via an MCP client.

Inspired by [`smtp-dev-server`](https://github.com/wrxck/smtp-dev-server) by
the same author. Single Go binary, no runtime dependencies.

> **Why?** When building apps that talk to Microsoft 365 (sending mail,
> creating Teams meetings), you need a way to test them in CI / staging
> without hitting real Microsoft. `microsoft-dev-server` captures everything
> your app sends and lets you inspect or assert on it.

## Install

```bash
go install github.com/wrxck/microsoft-dev-server/cmd/microsoft-dev-server@latest
```

Pre-built binaries are published on each release.

## Quick start

```bash
microsoft-dev-server
```

```
microsoft-dev-server dev
A fake Microsoft Graph API for development and testing.
HTTP API + Web UI: http://127.0.0.1:8080
```

Point your application at `http://127.0.0.1:8080` (in place of
`https://graph.microsoft.com`) and any token endpoint at
`http://127.0.0.1:8080/common/oauth2/v2.0/token`. Open
http://127.0.0.1:8080 in your browser to inspect captures.

## Endpoints implemented

| Endpoint | Behaviour |
|----------|-----------|
| `POST /v1.0/me/sendMail` | Captures the request body. Returns `202 Accepted` (matches Graph). |
| `POST /v1.0/me/onlineMeetings` | Returns a Graph-shaped `onlineMeeting` response with a fake `joinWebUrl` (`https://teams.microsoft.com/l/meetup-join/dev_<id>`). Captures the request. |
| `GET /v1.0/me` | Returns a canned identity (configurable via `--user-email`, `--user-name`, `--user-id`). |
| `POST /<tenant>/oauth2/v2.0/token` | Returns a fake bearer token (`access_token: dev_<random>`). Accepts any grant type. |

## Inspection / management endpoints

| Endpoint | Behaviour |
|----------|-----------|
| `GET /_dev/mail` | List of captured `sendMail` calls (newest first). |
| `GET /_dev/mail/{id}` | Single captured mail. |
| `DELETE /_dev/mail` | Clear all captured mail. |
| `GET /_dev/meetings` | List of captured online-meeting creates. |
| `GET /_dev/meetings/{id}` | Single captured meeting. |
| `DELETE /_dev/meetings` | Clear all captured meetings. |
| `GET /_dev/status` | Counts and configured user identity. |

## Configuration

Flags (all also accept matching env vars):

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--addr` | `ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `--max-items` | — | `500` | In-memory ring buffer size per kind |
| `--user-email` | `DEV_USER_EMAIL` | `rebecca@dev.local` | Identity returned from `/v1.0/me` |
| `--user-name` | `DEV_USER_NAME` | `Rebecca Dev` | Display name |
| `--user-id` | `DEV_USER_ID` | `00000000-0000-…-0001` | User id |

## MCP server

`microsoft-dev-server` also includes an MCP (Model Context Protocol) server
so an LLM client (e.g. Claude Code) can inspect captures and trigger
clears programmatically.

Run alongside the HTTP server:

```bash
# Terminal 1: the HTTP server
microsoft-dev-server

# Terminal 2: the MCP server (stdio)
microsoft-dev-server mcp --upstream http://127.0.0.1:8080
```

For Claude Code, add to `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "microsoft-dev": {
      "command": "microsoft-dev-server",
      "args": ["mcp", "--upstream", "http://127.0.0.1:8080"]
    }
  }
}
```

### Tools exposed

- `list_captured_mail` — list of captured sendMail calls
- `get_captured_mail(id)` — full captured mail
- `clear_captured_mail` — drop all captured mail
- `list_captured_meetings` — list of captured online-meeting creates
- `get_captured_meeting(id)` — full captured meeting
- `clear_captured_meetings` — drop all captured meetings
- `get_server_status` — counts + configured identity

## Wiring an app to use it

Most Microsoft Graph SDKs accept a custom base URL. For raw HTTP clients,
just point them at `http://127.0.0.1:8080` instead of
`https://graph.microsoft.com`.

For Node apps using the [`@azure/identity`](https://www.npmjs.com/package/@azure/identity)
+ [`@microsoft/microsoft-graph-client`](https://www.npmjs.com/package/@microsoft/microsoft-graph-client)
stack, set the `authorityHost` and `tokenAuthority` to the dev server and
override the Graph base URL with a custom `Middleware`.

## License

MIT — see [LICENSE.md](./LICENSE.md).
