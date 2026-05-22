# Security

SlideTalk uses a lightweight trust model for small self-hosted groups. It is not an SSO, billing, or public community platform.

## Local Token Identity

Each browser creates a random local token and sends it as a bearer token to the API. The server stores only a SHA-256 hash of that token in SQLite, not the raw token. Losing browser storage means losing that browser identity unless the operator restores data from backup.

The token is bearer-style: anyone with the token can act as that browser identity. Do not paste browser storage or request headers into chat, logs, screenshots, or issue reports.

## Admin Bootstrap Token

On startup, the server creates `~/.slidetalk/admin_token` when it does not already exist. Submitting that token promotes the current browser identity to site admin.

Keep this file private. It is the recovery path if all admins are demoted, and the app intentionally prevents demotion flows that would leave no admin recovery path.

The admin-token API is rate-limited and uses constant-time comparison for token checks.

## Room Passwords

Room passwords are optional. When set, passwords are hashed with bcrypt before storage. The raw room password is never returned by API responses.

Room join attempts with invalid passwords are rate-limited per caller, remote address, and room. A successful join clears that caller's failed-attempt counter.

Room passwords are meant to keep casual visitors out of a room URL. They are not a substitute for a private network, HTTPS, or careful sharing.

## WebSocket Tickets

Browsers request a short-lived, one-use WebSocket ticket before connecting to `/api/ws`. Tickets are bound to a room and user, expire after 60 seconds, and are consumed on first use.

Ticket creation is rate-limited per caller, remote address, and room. The client fetches a fresh ticket when reconnecting, so dropped sockets do not reuse old tickets.

## Migration Link Sensitivity

Room moderators can create migration links from room settings. The API returns a migration ID once, stores only a SHA-256 hash, and expires each link after 24 hours. A valid migration ID lets the holder join that specific room even when the room has a password.

Treat every migration ID as a bearer secret: it should not appear in logs, screenshots, analytics, or public issue reports.

## File Storage And Expiration

PDF, PNG, JPEG, WebP, and GIF slide files are stored under `~/.slidetalk/slides/` by SHA-256 and validated extension. Room references include expiration timestamps, and the server periodically cleans expired room references and unreferenced files.

Room moderators can upload, replace, or remove the slide file attached to their room. Site admins can inspect slide storage status and change slide expiration. Existing file content is deduplicated by hash.

The slide upload path enforces the configured maximum file size with `SLIDETALK_SLIDE_UPLOAD_LIMIT`, validates supported extension and detected content type, preserves the stored MIME type for serving, and rejects hash mismatches.

Audio files are stored under `~/.slidetalk/audio/` by SHA-256 and playlist rows are scoped to rooms. Moderators can upload, reorder, control, edit display metadata, and remove any room audio track. Uploaders can edit their own track title. Non-observer participants can upload and control playback only when the moderator enables the corresponding room settings. Observers can see and download room audio when audience audio access is enabled, but cannot upload or control playback.

Audio download links use random room-track bearer tokens in the URL so copied links work in external download managers. The server stores only token hashes, never raw tokens or browser auth tokens. These links remain valid until the associated room track is removed, so operators should treat copied audio URLs as room-scoped bearer secrets.

Non-admin audio uploads are capped by `SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT`; site admins can bypass that per-file limit after client confirmation. Slide and audio uploads are rejected when the server would have less free disk space than `SLIDETALK_MIN_FREE_SPACE` after retaining the upload. Audio tracks are garbage-collected after room age exceeds `SLIDETALK_AUDIO_FILES_GC_AFTER`.

## Browser And HTTP Constraints

SlideTalk sets security headers on HTTP responses:

- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- A Content Security Policy for same-origin assets, blob workers for PDF rendering, and same-origin WebSocket connections.
- A Permissions Policy that disables camera, microphone, and geolocation browser features.

HTTPS is recommended. Plain HTTP deployments remain usable for intranets, but browser clipboard APIs may be blocked; the UI shows a selected room-link field when automatic copy fails.

## Logging

The server logs startup and slide cleanup failures. It does not log raw auth tokens, room passwords, admin-token submissions, WebSocket tickets, audio download tokens, or migration IDs.
