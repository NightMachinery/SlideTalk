# SlideTalk Planning

Seed milestone plans live in `../plans/seed/`. Milestones 001 through 009 have created the runnable Go/Svelte foundation, local-token identity, a compact start flow with inline display-name editing and room-link name gating, bootstrap admin promotion, room create/join flows, realtime roundtable controls, current-speaker turns, a compact shared timer row with audio/visual timer-end feedback, manual-by-default raised-hand modes for new rooms, admin-only PDF and image slide storage with dedupe, per-room expiration, cleanup, manual-deletion reporting, shared slide viewing, moderator navigation sync, slide/markdown/audio room modes, synchronized shared audio with audio-only stage controls, configurable audio drift resync thresholds, tokenized external audio download links, persistent browser audio/slide caching with local usage and reset controls, editable audio display metadata, admin membership management, moderator room settings, per-participant audio upload/control grants, admin room-survival controls for room members, observer self-rejoin, immediate removed-room handling for kicked clients, room migration links, room slide/audio replacement/removal controls, global toast notifications, native-styled file uploads, a room-first workspace with a collapsible control rail and local keyboard shortcut configuration, no-Docker self-hosting with Caddy and tmux, and seed hardening for security headers, request limits, rate limits, reconnects, clipboard fallback, responsive UX, and regression coverage.

Start with `../plans/seed/000-index.md`. A fresh implementation context should read that index, implement the first unchecked milestone, update the checkbox only after verification passes, commit the milestone, and stop.

The current seed plan intentionally builds the product in layers:

1. Bootstrap the Go and Svelte app.
2. Add identity, admins, and rooms.
3. Add real-time participant coordination.
4. Add turns, timers, and hands.
5. Add slide storage.
6. Add slide viewing and markdown mode.
7. Add admin and room settings.
8. Add self-hosting.
9. Harden UX, security, and regression coverage.
