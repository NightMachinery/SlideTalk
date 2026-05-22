# Self Hosting

SlideTalk can run on a VPS without Docker by using the Go server, a Vite production build, tmux, and Caddy.

## Prerequisites

- Go 1.22 or newer.
- zsh with `nvm-load` available to non-interactive login shells.
- Node 20 through `nvm-load` and `nvm use 20`.
- pnpm.
- tmux.
- Caddy using `~/Caddyfile`.
- Ports `8097` and, for development mode, `5173` available on localhost.

The script checks for `go`, `pnpm` after loading Node, `tmux`, `caddy`, `zsh`, and `ss`.

## Commands

```bash
./self_host.zsh setup [url]
./self_host.zsh redeploy [url]
./self_host.zsh start [url]
./self_host.zsh stop
./self_host.zsh dev-start [url]
```

The default URL is `https://slidetalk.pinky.lilf.ir`. If a URL has no scheme, the script treats it as HTTPS. Use an explicit `http://` URL for plain HTTP hosting.

## First Setup

```bash
./self_host.zsh setup https://talk.example.com
```

`setup` stops existing SlideTalk tmux sessions, installs frontend dependencies with pnpm, builds `web/dist`, writes the managed Caddy block, reloads Caddy when it is running, and starts the Go server in tmux.

The production tmux session is named `slidetalk` by default.

## Redeploy

```bash
./self_host.zsh redeploy https://talk.example.com
```

`redeploy` stops SlideTalk sessions, runs `git pull --ff-only` when the directory is a Git worktree, reinstalls frontend dependencies with a frozen pnpm lockfile, rebuilds assets, updates Caddy, and starts production again.

## Start And Stop

```bash
./self_host.zsh start https://talk.example.com
./self_host.zsh stop
```

`start` stops production and development sessions, builds `web/dist` if it is missing, writes the production Caddy block, and starts the Go server. `stop` stops the managed tmux sessions and leaves the Caddy block intact.

## Development Mode

```bash
./self_host.zsh dev-start https://talk.example.com
```

`dev-start` stops production and development sessions, writes a development Caddy block, starts the Go server with `SLIDETALK_DEV=1`, and starts Vite with hot reload. The managed sessions are:

- `slidetalk-dev-api`
- `slidetalk-dev-web`

## Data Paths

The server stores runtime data under `~/.slidetalk` by default:

- Database: `~/.slidetalk/slidetalk.db`
- Admin bootstrap token: `~/.slidetalk/admin_token`
- Slides: `~/.slidetalk/slides/`
- Audio: `~/.slidetalk/audio/`

Set `SLIDETALK_DATA_DIR` before running the script to use another data directory. Set `SLIDETALK_ADDR` to change the Go listen address; the default is `127.0.0.1:8097`.

## Caddy Behavior

The script replaces only the block between these markers in `~/Caddyfile`:

```text
# BEGIN SLIDETALK
# END SLIDETALK
```

Production mode serves static files from `web/dist` through Caddy and reverse proxies `/api/*`, `/api/ws*`, and `/healthz` to the Go server.

Development mode reverse proxies app traffic to Vite and proxies `/api/*`, `/api/ws*`, and `/healthz` to the Go server.

For HTTPS URLs, the script writes an explicit HTTP-to-HTTPS redirect block. For HTTP URLs, it writes an explicit HTTPS-to-HTTP redirect block.

## HTTP Caveat

Browser clipboard and other secure-context APIs may be unavailable on plain HTTP except on localhost. SlideTalk still works over HTTP, but browser features that require HTTPS can need manual copy/paste fallback behavior.

The room-link copy control uses `navigator.clipboard` when the browser allows it. When clipboard access is blocked, the app shows a selected text field with the room link so users can copy manually.

## Security Headers And Limits

The Go server adds security headers to responses, including `nosniff`, `no-referrer`, a valid Permissions Policy, and a Content Security Policy that allows same-origin app assets, blob workers for PDF rendering, and WebSocket connections. The generated Caddy block replaces stale `Permissions-Policy` values so older `browsing-topics` entries do not trigger browser console warnings.

JSON request bodies are capped by the server. PDF, PNG, JPEG, WebP, and GIF slide uploads are capped by `SLIDETALK_SLIDE_UPLOAD_LIMIT`, which defaults to 200 MiB.

Audio uploads are capped for non-admin users by `SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT`, which defaults to 50 MiB. Site admins can upload larger audio files after a browser confirmation. Slide and audio uploads are rejected when retaining the upload would leave less free disk space than `SLIDETALK_MIN_FREE_SPACE`, which defaults to 0.5 GiB. Room audio tracks are cleaned after room age exceeds `SLIDETALK_AUDIO_FILES_GC_AFTER`, which defaults to 7 days.

Admin-token submissions, invalid room-password attempts, and WebSocket ticket creation are rate-limited.

## Proxy Environment

The script does not hardcode proxy values. When it launches tmux sessions, it preserves existing proxy-related environment variables using tmux `-e NAME=value` arguments. This includes common `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, lowercase variants, and pnpm/npm proxy variables when they are already set.

## Shell Behavior

The script runs Node and pnpm commands through non-interactive zsh login shells. This keeps `nvm-load` available without allowing deploy installs or builds to read from the terminal, which avoids suspended `redeploy` jobs in SSH or backgrounded shell contexts.

## Troubleshooting Occupied Ports

If startup reports that port `8097` or `5173` is occupied, find the process and stop it or choose a different port:

```bash
ss -ltnp | rg ':8097|:5173'
SLIDETALK_ADDR=127.0.0.1:18097 ./self_host.zsh start https://talk.example.com
SLIDETALK_VITE_PORT=15173 ./self_host.zsh dev-start https://talk.example.com
```

After changing `SLIDETALK_ADDR`, rerun `start` or `dev-start` so the Caddy block points at the same Go address.
