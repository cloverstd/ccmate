# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Backend
go generate ./...                              # Regenerate ent code (required after schema changes)
go run ./cmd/ccmate-server -config config.yaml # Start dev server on :8080
go test ./... -v                               # Run all tests
go test ./internal/scheduler/ -run TestIsValid  # Run a single test by name
go build -o bin/ccmate-server ./cmd/ccmate-server  # Build binary (includes embedded frontend)

# Frontend (web/)
cd web && bun install                          # Install deps
cd web && bun run dev                          # Dev server (proxies /api to :8080)
cd web && bun run build                        # Build to internal/static/dist/ (embedded in Go binary)
```

## Architecture

Single Go binary (`ccmate-server`) that integrates Coding Agents with GitHub project management. Listens for GitHub webhooks, schedules agent tasks, and creates PRs.

### Request Flow

```
GitHub Webhook → api/handler/webhook → gitprovider.VerifyWebhook() → webhook.Processor
  → creates Task(status=queued) in DB

Scheduler (5s poll loop) → finds queued tasks → checks project concurrency
  → transitions queued→running → spawns Runner.RunTask() in goroutine

Runner.RunTask():
  clone repo → checkout branch → build prompt (with UNTRUSTED_CONTEXT wrapping)
  → AgentAdapter.StartSession() → StreamEvents() → save events to DB
  → publish to SSE broker → git commit/push → create PR → mark succeeded

Web UI → GET /api/tasks/{id}/events/stream → SSE broker delivers real-time events
```

### Key Interfaces

- **`gitprovider.GitProvider`** (`internal/gitprovider/interface.go`) — Abstracts GitHub/GitLab/Gitee. GitHub impl in `gitprovider/github/`. Uses Registry pattern for provider registration.
- **`agentprovider.AgentAdapter`** (`internal/agentprovider/interface.go`) — Abstracts Claude Code/Codex/Gemini. Claude Code impl spawns `claude --print --output-format stream-json` subprocess. Mock impl for testing.

### Task State Machine

Defined in `internal/scheduler/statemachine.go`. Transitions enforced atomically via DB transaction:

```
queued → running → succeeded/failed/cancelled
                 → paused → queued (resume)
                 → waiting_user → running (user input)
failed → queued (retry)
```

### Data Layer

13 ent schemas in `internal/ent/schema/`. After editing schemas, run `go generate ./...` to regenerate the `internal/ent/` package. Core entities: Project, Task, Session, SessionMessage, SessionEvent, WebhookReceipt, CommandAudit.

### Prompt Security

`internal/prompt/builder.go` wraps all external content (issue body, comments, code, diffs) in `<UNTRUSTED_CONTEXT>` blocks. The platform system prompt is hardcoded and declares workspace/secret/network boundaries.

### SSE Broker

`internal/sse/broker.go` — Channel-based pub/sub keyed by topic (`task:{id}`). Runner publishes agent events; frontend subscribes via `/api/tasks/{id}/events/stream`.

### Frontend

React + TypeScript + Vite + Tailwind in `web/`. Built output embeds into Go binary via `//go:embed` in `internal/static/embed.go`. SPA fallback in `api/router.go` serves `index.html` for non-API routes.

Mobile adaptation is the top priority for frontend work. Design mobile-first before desktop enhancement. Prefer stacked layouts on narrow screens, wrapping tab and action rows, touch-friendly spacing, and avoid wide-table assumptions without a clear phone fallback.

## Configuration

YAML at project root (`config.yaml`), overridable by env vars with `CCMATE_` prefix (e.g., `CCMATE_SERVER_PORT=9090` → `server.port`). See `config.example.yaml` for all options. Config loaded by koanf in `internal/config/config.go`.

## Conventions

- Branch naming: `ccmate/issue-{issueNumber}-task-{taskID}`
- Webhook dedup: `delivery_id` stored in WebhookReceipt table
- One active task per issue (enforced at creation)
- Log sanitization: `internal/sanitize/` strips tokens/keys before DB storage
- Tests use table-driven pattern with subtests
