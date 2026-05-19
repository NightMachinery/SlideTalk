# 007 Admin And Room Settings Implementation Plan

> Implement after `006-slide-viewer-markdown` is checked in `plans/seed/000-index.md`.

**Goal:** Finish admin and moderator settings flows for operational control.

**Commit message:** `feat: add admin and room settings`

## Backend Behavior

- `GET /api/admins`:
  - Admin only.
  - Return admins as `{id, displayName, createdAt}`.
- `DELETE /api/admins/{userId}`:
  - Admin only.
  - Demotes one admin.
  - If this would leave zero admins and `~/.slidetalk/admin_token` is missing, reject.
- `POST /api/admins/demote-all`:
  - Admin only.
  - Demotes all admins except the caller by default.
  - If request body includes `{includeSelf:true}`, allow only when bootstrap token file exists.
- `PATCH /api/rooms/{roomId}/settings`:
  - Mod only.
  - Supports title, password set/clear, shared navigation default, no-slide mode, markdown participant editing, raise-hand mode.
- `PATCH /api/rooms/{roomId}/slide`:
  - Mod only, site admin only when changing expiration.
  - Allows changing current slide expiration.
- `POST /api/rooms/{roomId}/slide`:
  - Replaces old room slide reference with a new uploaded or deduped PDF.
- `DELETE /api/rooms/{roomId}/slide`:
  - Mod only.
  - Removes slide from room without deleting physical file unless cleanup later finds no unexpired refs.

## Frontend Behavior

- Profile/admin settings:
  - Show current admin status.
  - Admins can view other admins.
  - Admins can demote one admin.
  - Admins can use "demote all" with a confirmation step.
- Room settings panel for mods:
  - Rename room.
  - Set, change, or clear password.
  - Toggle no-slide mode.
  - Toggle participant markdown editing.
  - Toggle shared navigation default.
  - Change raise-hand mode.
  - Change slide expiration.
  - Replace or remove slide.
- All destructive actions require explicit confirmation in UI.

## Tests

- Non-admin cannot list or demote admins.
- Demote all does not accidentally remove every recovery path.
- Non-mod cannot patch room settings.
- Password clear makes room joinable without password.
- Slide replacement swaps room reference but keeps deduped file semantics.
- Removing slide does not delete a physical file still referenced elsewhere.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web build
```

## Acceptance Criteria

- Admins can manage admin membership.
- Mods can change all room settings after creation.
- Mods can replace or remove room slides.
- Backend authorization blocks every UI action when called by the wrong role.

