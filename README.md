# SlideTalk

SlideTalk is a self-hosted roundtable coordination app. It keeps the speaking order, observer queue, shared timer, slides, and optional discussion notes synchronized for groups that already have a separate audio call.

This repository is in the seed implementation phase. The current milestone provides the Go server, Svelte 5/Vite frontend shell, local browser-token identity, bootstrap admin promotion, and room create/join flows.

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
POST /api/rooms
GET /api/rooms/{roomId}
POST /api/rooms/{roomId}/join
POST /api/rooms/{roomId}/ws-ticket
GET /api/ws?ticket={ticket}
```

Start the frontend dev server:

```bash
pnpm --dir web dev
```

Vite proxies `/api` and `/healthz` to the Go server.

## Configuration

The Go server reads these environment variables:

- `SLIDETALK_ADDR`: listen address, default `127.0.0.1:8097`
- `SLIDETALK_DATA_DIR`: app data directory, default `~/.slidetalk`
- `SLIDETALK_PUBLIC_URL`: public URL used by later deployment milestones
- `SLIDETALK_DEV`: set to `1` for development mode

On startup, the server creates `~/.slidetalk/admin_token` if it does not already exist. Submit that token in the profile panel to promote the current browser identity to site admin.

Room participants connect to `/api/ws` with a one-time room-scoped ticket. Moderator commands currently support participant ordering, observer queue moves, role changes, and kicks.

## Planning

Seed milestone plans live in [plans/seed/000-index.md](plans/seed/000-index.md). A fresh context can continue the project with:

```text
Implement the next milestone.
```
