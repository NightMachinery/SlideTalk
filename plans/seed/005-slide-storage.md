# 005 Slide Storage Implementation Plan

> Implement after `004-turns-timers-hands` is checked in `plans/seed/000-index.md`.

**Goal:** Add admin-only PDF storage with local hashing, dedupe, expiration references, and manual deletion reporting.

**Commit message:** `feat: add slide storage and expiration`

## Data Model Changes

Add tables:

- `slide_files(sha256 text primary key, ext text not null, size_bytes integer not null, mime_type text not null, stored_path text not null, uploaded_by_user_id text not null, created_at text not null, missing_at text)`
- `room_slides(room_id text primary key, sha256 text not null, original_name text not null, expires_at text not null, uploaded_by_user_id text not null, created_at text not null, updated_at text not null)`

Only support `.pdf` in this milestone.

## Backend Behavior

- Create `~/.slidetalk/slides` on startup.
- `GET /api/slides/{sha256}`:
  - Site admin only.
  - Returns `{exists, sha256, alreadyUploaded, missing}`.
  - Checks DB and filesystem.
- `POST /api/slides`:
  - Site admin only.
  - Multipart fields: `file`, `sha256`, `expiresAt`, `roomId`, `originalName`.
  - Validate extension `.pdf`, MIME where available, size limit from config default 200 MB.
  - Compute server SHA-256 and reject if it differs from client hash.
  - Store at `~/.slidetalk/slides/{sha256}.pdf`.
  - If the physical file already exists and hash matches, do not rewrite it.
  - Upsert `room_slides` for the room.
- Expiration:
  - Each room reference has its own `expires_at`.
  - A cleanup job runs on startup and then hourly.
  - Delete physical file only when no unexpired `room_slides` rows reference it.
  - Delete expired room references.
- Manual deletion:
  - If DB references a file but the physical file is absent, set `missing_at` and expose slide status as manually deleted.
  - Do not recreate or silently ignore missing files.

## Frontend Behavior

- Admins see upload controls in room.
- Browser computes SHA-256 before upload using Web Crypto.
- If server reports the hash already exists, skip sending the file body and attach it to the room by metadata endpoint if implemented, or send a lightweight `POST /api/slides` without file using the known hash.
- Ask for expiration date during upload; default is now plus 14 days.
- Show upload progress, upload errors, and "file was deleted manually" state.

## Tests

- Server rejects non-admin upload.
- Server rejects non-PDF extension.
- Server rejects hash mismatch.
- Duplicate upload creates one physical file and multiple room references.
- Cleanup keeps file while any unexpired reference exists.
- Cleanup removes physical file after final reference expires.
- Missing physical file is reported as manually deleted.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web build
```

## Acceptance Criteria

- Only site admins can upload PDFs.
- Client hashes files before upload.
- Re-uploading the same PDF does not duplicate the stored file.
- Default expiration is two weeks.
- Manual file deletion is visible in the room instead of crashing.
