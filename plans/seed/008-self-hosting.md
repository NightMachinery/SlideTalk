# 008 Self Hosting Implementation Plan

> Implement after `007-admin-and-room-settings` is checked in `plans/seed/000-index.md`.

**Goal:** Add repeatable no-Docker self-hosting with Caddy and tmux.

**Commit message:** `chore: add self hosting script and docs`

## Files To Create Or Modify

- Create `self_host.zsh`
- Create `docs/self-hosting.md`
- Update `README.md`

## Script Interface

Command:

```bash
./self_host.zsh [setup|redeploy|start|stop|dev-start] [url]
```

Default URL: `https://slidetalk.pinky.lilf.ir`.

Commands:

- `setup`: stop existing sessions, install dependencies, build prod assets, write Caddy block, start prod.
- `redeploy`: stop, pull latest local worktree state only if already present, install/build, start prod.
- `start`: stop prod and dev sessions, build if needed, write prod Caddy block, start prod.
- `stop`: stop tmux sessions and leave Caddy block intact.
- `dev-start`: stop prod and dev sessions, write dev Caddy block, start Go server and Vite dev server with hot reload.

Do not use Docker.

## Required Script Details

- Use zsh.
- Include and use:

```zsh
tmuxnew () {
    tmux kill-session -t "$1" &> /dev/null || true
    tmux new -d -s "$@"
}
```

- For node:

```zsh
nvm-load
nvm use VERSION
```

- Use pnpm, not npm.
- Do not hardcode proxy variables. Preserve existing proxy environment variables when launching tmux sessions using hardened `tmux new -e "NAME=value"` syntax.
- Check required commands: `go`, `pnpm`, `tmux`, `caddy`.
- Check required ports are free before starting:
  - Go prod API default `8097`.
  - Vite dev default `5173`.
- If URL scheme is HTTPS:
  - Add Caddy site block for HTTPS URL.
  - Add explicit HTTP redirect block to HTTPS.
- If URL scheme is HTTP:
  - Add Caddy site block for HTTP URL.
  - Add explicit HTTPS redirect block to HTTP.
- Edit `~/Caddyfile` inside marked block:
  - `# BEGIN SLIDETALK`
  - `# END SLIDETALK`
- Prod Caddy:
  - Serve static files from `web/dist`.
  - Reverse proxy `/api/*` and `/healthz` to Go.
  - Reverse proxy WebSocket endpoint.
- Dev Caddy:
  - Proxy app traffic to Vite.
  - Proxy `/api/*`, `/healthz`, and WS to Go.
- Reload Caddy after block changes.

## Backend Support

- Ensure Go server config can:
  - Serve API-only behind Caddy.
  - Accept `SLIDETALK_ADDR`.
  - Accept `SLIDETALK_PUBLIC_URL`.
  - Accept `SLIDETALK_DEV=1`.

## Docs

`docs/self-hosting.md` must cover:

- Prerequisites.
- First setup.
- Redeploy.
- Start/stop.
- Dev mode.
- Data paths.
- Admin token path.
- Caddy block behavior.
- HTTP support and clipboard fallback caveat.
- Proxy environment behavior.
- Troubleshooting occupied ports.

## Tests

- Add shellcheck-compatible structure if shellcheck is available; if not, run `zsh -n self_host.zsh`.
- Unit test Go config environment parsing.
- Manual dry-run mode is useful but not required.

## Verification

```bash
zsh -n self_host.zsh
go test ./...
pnpm --dir web build
```

If available:

```bash
shellcheck self_host.zsh
```

## Acceptance Criteria

- `setup`, `start`, `stop`, `redeploy`, and `dev-start` are implemented.
- Prod mode uses Caddy for static files instead of a separate static file server.
- Dev mode hot reloads frontend changes.
- Script does not include hardcoded proxy values.
- Docs are sufficient for a sysadmin to run the app on HTTP or HTTPS.

