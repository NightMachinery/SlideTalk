# 003 Realtime Roundtable Implementation Plan

> Implement after `002-identity-admin-rooms` is checked in `plans/seed/000-index.md`.

**Goal:** Add deterministic live room state for participants, observers, mod ordering, role changes, and kicks.

**Commit message:** `feat: add realtime roundtable controls`

## Files To Create Or Modify

- Create `internal/realtime/hub.go`
- Create `internal/realtime/protocol.go`
- Create `internal/realtime/hub_test.go`
- Modify `internal/rooms/rooms.go`
- Modify `internal/httpserver/server.go`
- Add `web/src/lib/realtime.ts`
- Add room UI components under `web/src/lib/room/`

## Protocol

`POST /api/rooms/{roomId}/ws-ticket` returns `{ticket, expiresAt}`. Ticket is random, one-time use, expires in 60 seconds, and binds `{roomId,userId}`.

Client connects to `/api/ws?ticket={ticket}`.

Incoming command envelope:

```json
{"type":"people.reorder","requestId":"client-random-id","payload":{}}
```

Outgoing snapshot envelope:

```json
{"type":"room.snapshot","roomId":"room-id","version":12,"payload":{}}
```

Errors:

```json
{"type":"error","requestId":"client-random-id","code":"forbidden","message":"Only moderators can reorder people."}
```

## Snapshot Shape

Include:

- Room: `id`, `title`, `noSlideMode`, `allowParticipantMarkdown`.
- Caller: `userId`, `role`, `isAdmin`.
- Participants: ordered non-kicked members with role `mod` or `participant`.
- Observers: ordered non-kicked members with role `observer`.
- Placeholder fields for current speaker, timer, hands, slide, and markdown so later milestones do not redesign the snapshot.

## Commands

- `people.reorder`: mod only. Payload `{orderedUserIds, observerUserIds}`. Server validates all listed users are current non-kicked members and writes contiguous display orders.
- `people.setRole`: mod only. Payload `{userId, role}` where role is `mod`, `participant`, or `observer`. Cannot demote the last mod.
- `people.kick`: mod only. Payload `{userId}`. Sets `kicked_at`. Cannot kick the last mod.

Every accepted command increments a room version and broadcasts a fresh snapshot.

## Frontend Behavior

- Room view opens WebSocket after join.
- Show connection status: connected, reconnecting, disconnected.
- Render compact participant list and observer list in the same server order for every browser.
- Mods can:
  - Move participant up/down.
  - Move observer up/down.
  - Move person between participant and observer.
  - Kick person.
- Non-mods see read-only lists.

## Tests

- WebSocket ticket tests:
  - Missing/expired/used ticket is rejected.
  - Valid ticket connects to the correct room.
- Hub tests:
  - Joining sends a snapshot.
  - Reorder changes order for all connected clients.
  - Observer move is reflected in observer queue.
  - Last mod cannot be demoted or kicked.
  - Non-mod commands return `forbidden`.
- Race test:
  - Concurrent connects/disconnects and reorder commands do not race.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web build
```

## Acceptance Criteria

- Two browsers in the same room see the same participant and observer order.
- Mod changes are visible to all connected clients without refresh.
- Kicked users cannot remain connected or rejoin the room.
- Observer list and participant list can be collapsed independently in the UI.

