# 004 Turns, Timers, Hands Implementation Plan

> Implement after `003-realtime-roundtable` is checked in `plans/seed/000-index.md`.

**Goal:** Add turn-taking UX: current speaker, next speaker, timers, raise-hand modes, and keyboard shortcuts.

**Commit message:** `feat: add turn timer and hand controls`

## Data Model Changes

Add to `rooms`:

- `current_speaker_user_id text`
- `timer_state text not null default 'stopped'`
- `timer_duration_seconds integer not null default 0`
- `timer_started_at text`
- `raise_hand_mode text not null default 'off'`

Add table:

- `raised_hands(room_id text not null, user_id text not null, raised_at text not null, primary key(room_id,user_id))`

Allowed `timer_state`: `stopped`, `running`.

Allowed `raise_hand_mode`: `off`, `manual`, `queue`.

## Backend Behavior

- `turn.next`: mod only. Advances current speaker through participant order. Observers are skipped. If raise-hand mode is `queue` and any participant has a raised hand, the earliest raised hand becomes current speaker and that hand is cleared.
- `turn.previous`: mod only. Moves to previous participant in order.
- `turn.setCurrent`: mod only. Sets current speaker to a participant or mod, not observer.
- `timer.start`: mod only. Payload `{durationSeconds}`. Duration must be 1 to 86400. Store server start time.
- `timer.stop`: mod only. Stops timer.
- `timer.reset`: mod only. Stops and clears timer.
- `hand.raise`: participant or mod. Ignored if mode is `off`.
- `hand.lower`: caller can lower own hand; mod can lower anyone's.
- `settings.update`: mod only for `raiseHandMode`.

## Frontend Behavior

- Current speaker gets strong visual emphasis: border, background, and lucide `Mic` icon.
- Show "Up next: {name}" near the people list.
- Mods see buttons:
  - Previous speaker.
  - Next turn.
  - Start/stop/reset timer.
  - Raise-hand mode selector.
- Participants see raise/lower hand when mode is `manual` or `queue`.
- Timer is compact but legible and uses server time to compute remaining time locally.
- Keyboard shortcuts:
  - `b`: previous speaker, mod only.
  - `n`: next speaker, mod only.
  - `t`: start/stop timer, mod only. If no duration is set, use last selected local duration or 300 seconds.
  - `[` and `]`: reserved for slide previous/next and should do nothing until milestone 006.
- Ignore shortcuts while typing in inputs, textareas, or contenteditable elements.

## Tests

- Next/previous skips observers.
- Queue mode picks earliest raised hand and clears it.
- Manual mode does not auto-pick raised hands.
- Non-mod timer commands are forbidden.
- Timer snapshot includes enough data for clients to calculate remaining time.
- Shortcut handler ignores focused text inputs.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web build
```

## Acceptance Criteria

- Everyone sees the same current speaker and next speaker.
- Mods can advance turns from buttons and keyboard.
- Shared timer starts and stops for all clients.
- Participants can raise and lower hands according to the active mode.
