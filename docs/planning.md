# SlideTalk Planning

Seed milestone plans live in `../plans/seed/`. Milestones 001 through 009 have created the runnable Go/Svelte foundation, local-token identity, bootstrap admin promotion, room create/join flows, realtime roundtable controls, current-speaker turns, shared timers, raised-hand modes, admin-only PDF and image slide storage with dedupe, per-room expiration, cleanup, manual-deletion reporting, shared slide viewing, moderator navigation sync, no-slide markdown mode, admin membership management, moderator room settings, room migration links, room slide replacement/removal controls, a room-first slide workspace with a collapsible control rail and local keyboard shortcut configuration, no-Docker self-hosting with Caddy and tmux, and seed hardening for security headers, request limits, rate limits, reconnects, clipboard fallback, responsive UX, and regression coverage.

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
