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

The seed API index reserves a room migration-link endpoint, but this seed implementation does not expose a migration-link flow yet. Treat any future migration ID as a bearer secret: it should not appear in logs, screenshots, analytics, or public issue reports.

## File Storage And Expiration

PDF files are stored under `~/.slidetalk/slides/` by SHA-256. Room references include expiration timestamps, and the server periodically cleans expired room references and unreferenced files.

Site admins can inspect slide storage status. Room moderators can attach or remove a room's slide reference. Existing file content is deduplicated by hash.

The upload path enforces the configured maximum file size with `SLIDETALK_SLIDE_MAX_BYTES`, validates PDF extension/content shape, and rejects hash mismatches.

## Browser And HTTP Constraints

SlideTalk sets security headers on HTTP responses:

- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- A Content Security Policy for same-origin assets, blob workers for PDF rendering, and same-origin WebSocket connections.

HTTPS is recommended. Plain HTTP deployments remain usable for intranets, but browser clipboard APIs may be blocked; the UI shows a selected room-link field when automatic copy fails.

## Logging

The server logs startup and slide cleanup failures. It does not log raw auth tokens, room passwords, admin-token submissions, WebSocket tickets, or migration IDs.
