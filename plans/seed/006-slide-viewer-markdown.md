# 006 Slide Viewer And Markdown Mode Implementation Plan

> Implement after `005-slide-storage` is checked in `plans/seed/000-index.md`. Use `gpt-taste` before shaping the combined slide, people, timer, and markdown workspace.

**Goal:** Add the main shared workspace: PDF viewing, mod-driven shared navigation, local follow toggle, and no-slide markdown mode.

**Commit message:** `feat: add shared slide and markdown views`

## Data Model Changes

Add to `rooms`:

- `slide_page integer not null default 1`
- `shared_navigation_enabled integer not null default 1`
- `markdown_updated_by_user_id text`
- `markdown_updated_at text`

## Backend Behavior

- `GET /api/rooms/{roomId}/slide/file`:
  - Requires room membership.
  - Streams the current PDF if present and not missing.
  - Returns 404 problem JSON if no slide or manually deleted.
- `slide.navigate`:
  - Mod only for broadcast navigation.
  - Payload `{page, modSharedNavigationEnabled}`.
  - Validate page is positive.
  - If `modSharedNavigationEnabled` is false, do not update server shared page and do not broadcast page changes.
  - If true, update `rooms.slide_page` and broadcast snapshot.
- `settings.update`:
  - Mod only for `sharedNavigationEnabled`, `noSlideMode`, `allowParticipantMarkdown`.
- `markdown.update`:
  - Mod allowed.
  - Participant allowed only if `allowParticipantMarkdown` is true.
  - Sanitize output on frontend render, but still enforce size max 64 KB server-side.

## Frontend Behavior

- Main room layout:
  - Center area: slide viewer or markdown panel.
  - Side area: compact people list and timer.
  - Participant and observer lists collapse independently.
- PDF:
  - Use bundled `pdfjs-dist`.
  - Render current page client-side.
  - Users can navigate locally.
  - Mods have a "share navigation" checkbox, enabled by default.
  - Every browser has a local "follow moderator navigation" checkbox, enabled by default.
  - When a mod navigates and the mod checkbox is enabled, the server broadcasts the page. Clients apply it only when local follow is enabled.
- No-slide mode:
  - People list gets more space.
  - Central area renders sanitized markdown.
  - Mods can edit markdown.
  - If enabled, participants can edit too.
  - Use last-write-wins updates with visible "last edited by" metadata.
- Keyboard:
  - `[` and `]` navigate slides for mods with shared navigation enabled.
  - Non-mods use local navigation controls only.

## Tests

- Room member can fetch current PDF; non-member cannot.
- Missing file returns the manual deletion state.
- Mod shared navigation disabled means no broadcast page change.
- Local follow disabled means client ignores incoming shared page.
- Participant markdown edit is forbidden unless room setting allows it.
- Markdown render escapes script injection.

## Verification

```bash
go test ./...
go test -race ./...
pnpm --dir web build
```

## Acceptance Criteria

- Everyone can view the current room PDF.
- Users can navigate locally.
- Mod navigation pulls followers back to the same page only when both toggles are enabled.
- No-slide mode displays shared markdown in the central area.
- Markdown rendering does not execute raw scripts.
