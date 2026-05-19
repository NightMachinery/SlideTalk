# 009 Hardening Implementation Plan

> Implement after `008-self-hosting` is checked in `plans/seed/000-index.md`. Use `gpt-taste` during the UX hardening pass before changing visual layout, spacing, or motion.

**Goal:** Harden SlideTalk for real self-hosted use: security, responsive UX, intranet constraints, and full regression coverage.

**Commit message:** `test: harden slidetalk seed`

## Security Hardening

- Add security headers:
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
  - `Content-Security-Policy` that permits local app assets, blob URLs needed for PDF rendering, and WebSocket to same origin.
- Ensure all SQL uses parameters.
- Ensure all user-controlled strings rendered as text or sanitized markdown.
- Add request body limits.
- Add upload size config and enforcement.
- Rate-limit:
  - Admin token submission.
  - Room password attempts.
  - WebSocket ticket creation.
- Ensure logs do not include raw auth tokens, room passwords, admin token submissions, or migration IDs.

## UX Hardening

- Desktop:
  - Center slide or markdown area dominates.
  - People and timer remain compact and legible.
  - No nested card-heavy layout.
- Mobile:
  - Slide/markdown first.
  - People and observers collapsible.
  - Timer and current speaker always reachable.
- Clipboard:
  - Try `navigator.clipboard`.
  - On failure or HTTP restriction, show selected text field fallback.
- WebSocket:
  - Reconnect with backoff.
  - On reconnect, fetch a new WS ticket and wait for a fresh snapshot.
  - Show stale/disconnected state without losing local identity.
- Accessibility:
  - Buttons have labels.
  - Keyboard shortcuts do not trap typing.
  - Current speaker is indicated by text/icon and not color alone.

## Integration Tests

- Two-browser room:
  - Both browsers see same order.
  - Mod reorder syncs.
  - Observer move syncs.
  - Kick disconnects target.
- Turns:
  - Current speaker and up-next match across browsers.
  - Timer state survives reconnect.
  - Raise-hand queue picks earliest raised participant.
- Slides:
  - Hash preflight avoids duplicate upload.
  - Local navigation does not affect others.
  - Mod shared navigation realigns followers.
  - Local follow disabled ignores shared page events.
- Markdown:
  - Mod edits render everywhere.
  - Participant edits depend on setting.
  - XSS payload is escaped or removed.
- Deployment:
  - `zsh -n self_host.zsh`.
  - Caddy block writer is idempotent.

## Documentation

- Update `README.md` with:
  - What SlideTalk is and is not.
  - Local development.
  - Production deployment pointer.
  - Data storage paths.
- Update `docs/self-hosting.md` with any script behavior changed during hardening.
- Add `docs/security.md` covering:
  - Lightweight local-token auth model.
  - Admin bootstrap token.
  - Room passwords.
  - Migration link sensitivity.
  - File storage and expiration.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web test
pnpm --dir web build
zsh -n self_host.zsh
```

If browser test tooling has been added, run the full browser test command documented in `README.md`.

## Acceptance Criteria

- All documented verification commands pass.
- App uses no runtime external fonts, scripts, icons, captcha, or CDN assets.
- HTTP deployment remains usable, including copy-link fallback.
- WebSocket scheme is dynamic based on the current page URL.
- The UI remains usable on desktop and mobile.
- Security-sensitive values are not leaked in logs or API responses.
