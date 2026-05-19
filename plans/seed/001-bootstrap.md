# 001 Bootstrap Implementation Plan

> Required sub-skill for implementation: use the Go, Svelte, TDD, and verification workflows relevant to this repo. Follow `plans/seed/000-index.md`.

**Goal:** Create the runnable Go plus Svelte 5 foundation for SlideTalk.

**Commit message:** `chore: scaffold slidetalk app`

## Files To Create

- `go.mod`
- `cmd/slidetalk/main.go`
- `internal/config/config.go`
- `internal/httpserver/server.go`
- `internal/httpserver/server_test.go`
- `web/package.json`
- `web/pnpm-lock.yaml`
- `web/index.html`
- `web/vite.config.ts`
- `web/tsconfig.json`
- `web/src/main.ts`
- `web/src/App.svelte`
- `web/src/app.css`
- `README.md`
- `docs/planning.md`

## Implementation Steps

1. Initialize Go module `github.com/NightMachinery/SlideTalk`.
2. Create `internal/config` with:
   - `DataDir`, default `~/.slidetalk`.
   - `Addr`, default `127.0.0.1:8097`.
   - `PublicURL`, default empty.
   - `DevMode`, from `SLIDETALK_DEV=1`.
3. Create `internal/httpserver` with:
   - `GET /healthz` returning `{"ok":true}`.
   - API 404 responses as `application/problem+json`.
   - Optional static file serving from `web/dist` when the directory exists.
4. Create `cmd/slidetalk/main.go`:
   - Load config.
   - Ensure data directory exists.
   - Start HTTP server.
   - Gracefully shut down on SIGINT/SIGTERM.
5. Scaffold Svelte 5 plus Vite in `web/`:
   - Use pnpm.
   - Add dependencies: `@sveltejs/vite-plugin-svelte`, `svelte`, `vite`, `typescript`, `vitest`, `@testing-library/svelte`, `jsdom`, `lucide-svelte`.
   - Add scripts: `dev`, `build`, `preview`, `test`.
6. Build a minimal app shell:
   - Header with "SlideTalk".
   - Empty room list state.
   - Basic responsive layout.
   - Use one lucide icon to verify icon wiring.
7. Add docs:
   - `README.md` with dev commands.
   - `docs/planning.md` pointing to `plans/seed/000-index.md`.

## Tests

- `internal/httpserver/server_test.go`:
  - `GET /healthz` returns 200 and `{"ok":true}`.
  - Unknown `/api/missing` returns problem JSON 404.
- Frontend:
  - Add a Vitest smoke test only if it does not create framework churn. Otherwise make `pnpm --dir web build` the frontend verification for this milestone.

## Verification

Run:

```bash
go test ./...
pnpm --dir web install
pnpm --dir web build
```

Expected:

- Go tests pass.
- pnpm creates `web/pnpm-lock.yaml`.
- Vite build writes `web/dist`.

## Acceptance Criteria

- `go run ./cmd/slidetalk` starts on `127.0.0.1:8097`.
- `curl http://127.0.0.1:8097/healthz` returns `{"ok":true}`.
- `pnpm --dir web dev` starts the Svelte app.
- `README.md` explains local Go and Svelte commands.

