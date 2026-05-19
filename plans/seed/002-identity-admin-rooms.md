# 002 Identity, Admins, Rooms Implementation Plan

> Implement after `001-bootstrap` is checked in `plans/seed/000-index.md`.

**Goal:** Add persistent lightweight identity, bootstrap admins, and room creation/joining with optional passwords.

**Commit message:** `feat: add identity admins and rooms`

## Files To Create Or Modify

- Create `internal/store/store.go`
- Create `internal/store/migrations.go`
- Create `internal/auth/auth.go`
- Create `internal/rooms/rooms.go`
- Modify `internal/httpserver/server.go`
- Modify Svelte app files under `web/src/`
- Update `README.md` and `docs/planning.md`

## Data Model

SQLite tables:

- `users(id text primary key, token_hash text unique not null, display_name text not null, is_admin integer not null default 0, created_at text not null, updated_at text not null)`
- `rooms(id text primary key, title text not null, password_hash text, no_slide_mode integer not null default 0, markdown text not null default '', allow_participant_markdown integer not null default 0, created_by_user_id text not null, created_at text not null, updated_at text not null)`
- `room_members(room_id text not null, user_id text not null, role text not null, display_order integer not null, joined_at text not null, kicked_at text, primary key(room_id,user_id))`

Allowed member roles: `mod`, `participant`, `observer`.

## Backend Behavior

- Accept `Authorization: Bearer <rawToken>` on all `/api/*` routes except health.
- Hash raw tokens with SHA-256 before storage.
- `GET /api/me`:
  - Creates a user if the token hash is new.
  - Returns `{id, displayName, isAdmin}`.
- `PATCH /api/me`:
  - Validates display name: trim, 1 to 80 characters.
  - Saves display name.
- Admin bootstrap:
  - On startup, ensure `~/.slidetalk/admin_token` exists.
  - If missing, create a 32-byte random URL-safe token and chmod `0600`.
  - `POST /api/me/admin-token` with `{token}` promotes caller if token matches.
  - Rate-limit failed submissions by IP plus user token in memory.
- `POST /api/rooms`:
  - Requires display name.
  - Body: `{title, password?}`.
  - Creator becomes `mod` with `display_order = 0`.
  - Password is optional. Hash with Argon2id or bcrypt.
- `POST /api/rooms/{roomId}/join`:
  - Requires display name.
  - If room has password, require correct `{password}`.
  - Existing members rejoin unless kicked.
  - New users join as participants at the end.
- `GET /api/rooms/{roomId}`:
  - Return room metadata and caller membership state. Do not return password hash.

## Frontend Behavior

- Generate a 32-byte random local auth token in localStorage key `slidetalk.authToken`.
- Store API client in `web/src/lib/api.ts`.
- Show profile setup if `displayName` is blank.
- Add screens:
  - Home: create room form.
  - Join room: password prompt when needed.
  - Room shell with joined-state summary after joining.
  - Profile settings with admin token submission.
- Persist the display name server-side so refresh does not prompt again.

## Tests

- Store migration test creates all tables.
- Auth test confirms same token returns same user and token hash is not raw token.
- Admin token tests:
  - Token file is created if missing.
  - Correct token promotes user.
  - Incorrect token does not promote user.
- Room tests:
  - Creator is mod.
  - Password room rejects missing/wrong password.
  - Correct password joins.
  - Kicked members cannot rejoin.

## Verification

```bash
go test ./...
pnpm --dir web build
```

## Acceptance Criteria

- First visit creates a backend user from the local browser token.
- User can set a display name once and refresh without being re-prompted.
- User can create a room, become mod, and rejoin it.
- Optional room password is enforced.
- A valid `~/.slidetalk/admin_token` promotes the caller to site admin.
