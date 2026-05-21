# SlideTalk

SlideTalk is a self-hosted roundtable coordination app. It keeps the speaking order, observer queue, shared timer, slides, and optional discussion notes synchronized for groups that already have a separate audio call.

SlideTalk is not an audio/video conferencing system, a public event platform, or an account-management service. It assumes a small trusted group, a separate call, and an operator who controls the host.

This repository is in the seed implementation phase. The current milestone provides the Go server, Svelte 5/Vite frontend shell, local browser-token identity, bootstrap admin promotion, room create/join flows, realtime roundtable controls, turn selection, shared timers, hand-raise queues, PDF slide storage with expiration cleanup, shared PDF viewing, no-slide markdown mode, admin membership controls, moderator room settings, room migration links, slide replacement/removal, and no-Docker self-hosting with Caddy and tmux.

## Requirements

- Go 1.22 or newer
- Node through the local `nvm-load` workflow when using zsh
- pnpm

## Development

Install frontend dependencies:

```bash
pnpm --dir web install
```

Run backend tests:

```bash
go test ./...
```

Build the frontend:

```bash
pnpm --dir web build
```

Start the Go server:

```bash
go run ./cmd/slidetalk
```

The server listens on `127.0.0.1:8097` by default and exposes:

```text
GET /healthz
GET /api/me
PATCH /api/me
POST /api/me/admin-token
GET /api/admins
DELETE /api/admins/{userId}
POST /api/admins/demote-all
POST /api/rooms
GET /api/rooms/{roomId}
GET /api/rooms/{roomId}/snapshot
POST /api/rooms/{roomId}/join
PATCH /api/rooms/{roomId}/settings
POST /api/rooms/{roomId}/ws-ticket
POST /api/rooms/{roomId}/slide
PATCH /api/rooms/{roomId}/slide
DELETE /api/rooms/{roomId}/slide
GET /api/rooms/{roomId}/slide/file
GET /api/slides/{sha256}
POST /api/slides
GET /api/ws?ticket={ticket}
```

Start the frontend dev server:

```bash
pnpm --dir web dev
```

Vite proxies `/api` and `/healthz` to the Go server.

For a full local verification pass:

```bash
go test ./...
go test -race ./...
pnpm --dir web test
pnpm --dir web build
zsh -n self_host.zsh
zsh scripts/test_self_host_shell.zsh
```

## Self Hosting

Run SlideTalk behind Caddy with tmux:

```bash
./self_host.zsh setup [url]
./self_host.zsh redeploy [url]
./self_host.zsh start [url]
./self_host.zsh stop
./self_host.zsh dev-start [url]
```

The default URL is `https://slidetalk.pinky.lilf.ir`. Production mode serves `web/dist` directly from Caddy and proxies API/WebSocket traffic to the Go server. Development mode proxies app traffic to Vite for hot reload.

See [docs/self-hosting.md](docs/self-hosting.md) for prerequisites, data paths, Caddy behavior, HTTP caveats, proxy environment handling, and troubleshooting.

## Configuration

The Go server reads these environment variables:

- `SLIDETALK_ADDR`: listen address, default `127.0.0.1:8097`
- `SLIDETALK_DATA_DIR`: app data directory, default `~/.slidetalk`
- `SLIDETALK_PUBLIC_URL`: public URL used by self-hosting and deployment workflows
- `SLIDETALK_DEV`: set to `1` for development mode
- `SLIDETALK_SLIDE_MAX_BYTES`: PDF upload limit, default `209715200`

On startup, the server creates `~/.slidetalk/admin_token` and `~/.slidetalk/slides` if they do not already exist. Submit that token in the profile panel to promote the current browser identity to site admin.

Runtime data lives under `~/.slidetalk` by default:

- `~/.slidetalk/slidetalk.db`
- `~/.slidetalk/admin_token`
- `~/.slidetalk/slides/`

Room participants fetch an initial room snapshot from `/api/rooms/{roomId}/snapshot`, then connect to `/api/ws` with a one-time room-scoped ticket for live updates. Moderator commands currently support participant ordering, observer queue moves, role changes, kicks, current-speaker navigation, server-timed countdowns, manual or queue-based raised hands, shared slide navigation, markdown updates, room settings, password changes, and slide replacement/removal. Room moderators can upload or replace the PDF attached to their room. Site admins can manage admin membership, inspect slide storage status, and change existing slide expiration. The browser hashes files before upload, the server stores files by SHA-256, and expired room references are cleaned up hourly.

Room moderators can create 24-hour migration links from room settings. A migration ID is a bearer secret that is shown once to the issuing browser, lets the holder join that room even when it has a password, and is stored in SQLite only as a SHA-256 hash.

Security notes live in [docs/security.md](docs/security.md).

## Planning

Seed milestone plans live in [plans/seed/000-index.md](plans/seed/000-index.md). A fresh context can continue the project with:

```text
Implement the next milestone.
```
