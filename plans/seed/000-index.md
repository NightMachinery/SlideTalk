# SlideTalk Seed Plan Index

> For agentic workers: read this file first when the user says "Implement the next milestone." Implement only the first unchecked milestone below, then update this file, run the milestone verification, commit the milestone, and stop.

## Product Goal

SlideTalk is a self-hosted Go plus Svelte 5 roundtable coordination app. It keeps participant order, observers, turns, timers, slides, and optional shared markdown synchronized across browsers while the actual audio call happens elsewhere.

## Global Decisions

- Backend: single Go 1.22+ binary, SQLite with WAL, standard `net/http`, Gorilla WebSocket or `nhooyr.io/websocket`.
- Frontend: Svelte 5 plus Vite, pnpm, lucide icons, bundled `pdfjs-dist`, no runtime external assets.
- Storage root: `~/.slidetalk`.
- Database: `~/.slidetalk/slidetalk.db`.
- Slides: `~/.slidetalk/slides/{sha256}.{ext}`.
- Admin bootstrap token: generated at `~/.slidetalk/admin_token`.
- JSON: camelCase.
- Auth: browser-local random token in localStorage; backend stores a hash, not the raw token.
- Deployment: no Docker. Use tmux and Caddy.
- Network: WebSocket URL must use `ws` on HTTP and `wss` on HTTPS.

## Next Milestone Rules

1. Read this file and find the first unchecked item in the milestone checklist.
2. Read that milestone file completely.
3. Implement only that milestone.
4. Update any docs listed in the milestone.
5. Run the verification commands from the milestone.
6. Mark that milestone checked here only after verification passes.
7. Commit all files for that milestone with the exact commit message from the milestone.
8. Stop and report the commit hash, verification commands, and any residual risk.

If the worktree is dirty before implementation, inspect `git status --short`. Do not overwrite unrelated user changes. If existing changes are unrelated, leave them alone. If they block the milestone, ask the user.

## Milestone Checklist

- [ ] 001 Bootstrap: `plans/seed/001-bootstrap.md`
- [ ] 002 Identity, Admins, Rooms: `plans/seed/002-identity-admin-rooms.md`
- [ ] 003 Realtime Roundtable: `plans/seed/003-realtime-roundtable.md`
- [ ] 004 Turns, Timers, Hands: `plans/seed/004-turns-timers-hands.md`
- [ ] 005 Slide Storage: `plans/seed/005-slide-storage.md`
- [ ] 006 Slide Viewer And Markdown Mode: `plans/seed/006-slide-viewer-markdown.md`
- [ ] 007 Admin And Room Settings: `plans/seed/007-admin-and-room-settings.md`
- [ ] 008 Self Hosting: `plans/seed/008-self-hosting.md`
- [ ] 009 Hardening: `plans/seed/009-hardening.md`

## Public API Shape

### HTTP

- `GET /healthz`
- `GET /api/me`
- `PATCH /api/me`
- `POST /api/me/admin-token`
- `GET /api/admins`
- `DELETE /api/admins/{userId}`
- `POST /api/admins/demote-all`
- `POST /api/rooms`
- `GET /api/rooms/{roomId}`
- `POST /api/rooms/{roomId}/join`
- `PATCH /api/rooms/{roomId}/settings`
- `POST /api/rooms/{roomId}/migration-link`
- `POST /api/rooms/{roomId}/ws-ticket`
- `POST /api/slides`
- `GET /api/slides/{sha256}`
- `POST /api/rooms/{roomId}/slide`
- `PATCH /api/rooms/{roomId}/slide`
- `DELETE /api/rooms/{roomId}/slide`
- `GET /api/rooms/{roomId}/slide/file`
- `GET /api/ws?ticket={ticket}`

### WebSocket Commands

- `people.reorder`
- `people.setRole`
- `people.kick`
- `turn.next`
- `turn.previous`
- `turn.setCurrent`
- `timer.start`
- `timer.stop`
- `timer.reset`
- `hand.raise`
- `hand.lower`
- `slide.navigate`
- `markdown.update`
- `settings.update`

### WebSocket Events

- `room.snapshot`
- `error`

All accepted commands should produce a fresh `room.snapshot` broadcast to every connected client in the room.

## Shared Verification

Every milestone should run the commands available at that point:

- `go test ./...`
- `pnpm --dir web test` once frontend tests exist.
- `pnpm --dir web build` once the web app exists.
- `go test -race ./...` once WebSocket or concurrent state exists.

Do not claim completion without fresh command output.

