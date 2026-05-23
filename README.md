# SlideTalk

SlideTalk is a self-hosted roundtable coordination app. It keeps the speaking order, observer queue, shared timer, slides, optional discussion notes, and shared audio playback synchronized for small groups.

SlideTalk is not a video conferencing system, a public event platform, or an account-management service. It assumes a small trusted group and an operator who controls the host.

This repository is in the seed implementation phase. The current milestone provides the Go server, Svelte 5/Vite frontend shell, local browser-token identity, a compact start flow with inline display-name editing, bootstrap admin promotion, room create/join flows, realtime roundtable controls, turn selection, a compact shared timer row with end-of-timer feedback, manual-by-default hand raising for participants and moderators, PDF and image slide storage with room-level expiration cleanup, shared slide viewing, no-slide markdown mode, synchronized room audio with audio-only mode, private per-user audio track stars, admin membership controls, moderator room settings, observer self-rejoin, immediate removed-room handling for kicked clients, room migration links, slide/audio replacement and removal, local browser cache controls, global toast notifications, server-side room file inspection/expiration CLI commands, and no-Docker self-hosting with Caddy and tmux.

## Requirements

- Go 1.22 or newer
- Node through the local `nvm-load` workflow when using zsh
- pnpm

## Development

Install frontend dependencies:

```bash
pnpm --dir web install
```

Run backend tests:

```bash
go test ./...
```

Build the frontend:

```bash
pnpm --dir web build
```

Start the Go server:

```bash
go run ./cmd/slidetalk
```

The server listens on `127.0.0.1:8097` by default and exposes:

```text
GET /healthz
GET /api/me
PATCH /api/me
POST /api/me/admin-token
GET /api/admins
DELETE /api/admins/{userId}
POST /api/admins/demote-all
POST /api/rooms
GET /api/rooms/{roomId}
GET /api/rooms/{roomId}/snapshot
POST /api/rooms/{roomId}/join
PATCH /api/rooms/{roomId}/settings
POST /api/rooms/{roomId}/ws-ticket
POST /api/rooms/{roomId}/slide
PATCH /api/rooms/{roomId}/slide
DELETE /api/rooms/{roomId}/slide
GET /api/rooms/{roomId}/slide/file
POST /api/rooms/{roomId}/audio
PATCH /api/rooms/{roomId}/audio/{trackId}
POST /api/rooms/{roomId}/audio/{trackId}/download-link
GET /api/rooms/{roomId}/audio/{trackId}
GET /api/rooms/{roomId}/audio/{trackId}/cover
DELETE /api/rooms/{roomId}/audio/{trackId}
GET /api/slides/{sha256}
POST /api/slides
GET /api/ws?ticket={ticket}
```

Start the frontend dev server:

```bash
pnpm --dir web dev
```

Vite proxies `/api` and `/healthz` to the Go server.

For a full local verification pass:

```bash
go test ./...
go test -race ./...
pnpm --dir web test
pnpm --dir web build
zsh -n self_host.zsh
zsh scripts/test_self_host_shell.zsh
```

## Self Hosting

Run SlideTalk behind Caddy with tmux:

```bash
./self_host.zsh setup [url]
./self_host.zsh redeploy [url]
./self_host.zsh start [url]
./self_host.zsh stop
./self_host.zsh dev-start [url]
```

The default URL is `https://slidetalk.pinky.lilf.ir`. Production mode serves `web/dist` directly from Caddy and proxies API/WebSocket traffic to the Go server. Development mode proxies app traffic to Vite for hot reload.

See [docs/self-hosting.md](docs/self-hosting.md) for prerequisites, data paths, Caddy behavior, HTTP caveats, proxy environment handling, and troubleshooting.

## Configuration

The Go server reads these environment variables:

- `SLIDETALK_ADDR`: listen address, default `127.0.0.1:8097`
- `SLIDETALK_DATA_DIR`: app data directory, default `~/.slidetalk`
- `SLIDETALK_PUBLIC_URL`: public URL used by self-hosting and deployment workflows
- `SLIDETALK_DEV`: set to `1` for development mode
- `SLIDETALK_SLIDE_UPLOAD_LIMIT`: slide upload limit, default `200m`
- `SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT`: non-admin audio upload limit, default `50m`
- `SLIDETALK_ROOM_GC_AFTER`: default room uploaded-file retention, default `7d`
- `SLIDETALK_AUDIO_FILES_GC_AFTER`: deprecated fallback for room retention when `SLIDETALK_ROOM_GC_AFTER` is unset, default `7d`
- `SLIDETALK_AUDIO_DRIFT_THRESHOLD`: client resync threshold for shared audio drift, default `3s`
- `SLIDETALK_MIN_FREE_SPACE`: reject retained uploads that would leave less free disk space, default `0.5GB`

On startup, the server creates `~/.slidetalk/admin_token` and `~/.slidetalk/slides` if they do not already exist. Submit that token from the collapsed Admin section on the start page to promote the current browser identity to site admin.

Runtime data lives under `~/.slidetalk` by default:

- `~/.slidetalk/slidetalk.db`
- `~/.slidetalk/admin_token`
- `~/.slidetalk/slides/`
- `~/.slidetalk/audio/`

Room participants fetch an initial room snapshot from `/api/rooms/{roomId}/snapshot`, then connect to `/api/ws` with a one-time room-scoped ticket for live updates. Moderator commands currently support participant ordering, observer queue moves, role changes, kicks, current-speaker navigation, server-timed countdowns, manual or queue-based raised hands, shared slide navigation, markdown updates, room settings, password changes, slide replacement/removal, audio playlist controls, and synchronized audio play/pause/seek.

Room moderators can upload or replace the PDF, PNG, JPEG, WebP, or GIF slide file attached to their room. Room uploaded files use a room-level survival deadline shown in the Cache panel. Room moderators can prolong survival up to 10 days from now. Site admins who are room moderators can shorten survival no earlier than 24 hours from now, or set the room to never expire. Rooms use a single mode setting: slides, markdown, or audio. In audio mode, the shared audio player, playlist, finish behavior, and upload controls move into the main stage instead of a right-side audio rail. Room members can see and download audio by default. Optional room settings let non-observer participants upload audio and separately control playback; anyone with audio control access can change Finish behavior. Observers cannot upload or control playback. Site admins bypass the audio per-file size limit after a styled confirmation, but all uploads remain subject to the minimum free-space floor. Audio uploads support common browser-safe formats including MP3, M4A, AAC, Ogg/Opus, WAV, FLAC, and WebM audio; the browser skips clearly unsupported selections before upload while the server remains the authoritative validator.

Audio downloads can be converted into room-track download links with a random bearer token in the URL. These links are intended for external download managers and remain valid until the track is removed from the room; the server stores only a hash of the token and does not include browser auth tokens or user IDs in the URL. Each room member can privately star tracks and filter the playlist to starred tracks. Moderators can enable aggregate star counts for the room. Browsers stream uncached audio immediately, fill persistent IndexedDB audio and slide caches in the background, seed those caches directly after successful local uploads, reuse cached blobs for playback/manual downloads/slide viewing after refresh, remember each room's local mute state in localStorage, and resync shared playback when local drift exceeds `SLIDETALK_AUDIO_DRIFT_THRESHOLD`. Each browser cache garbage-collects entries older than 30 days and trims oldest entries beyond 2 GiB or 200 files. Active rooms include a Cache section in the control rail for local audio/slide usage totals, server-side room survival, prolong controls, and reset controls.

Server operators can inspect and force-expire room uploaded files with the installed CLI:

```bash
slidetalk ls --sort=size
slidetalk ls --sort=created-date
slidetalk rm ROOM_ID
slidetalk rm ROOM_ID -y
```

`slidetalk ls` shows password status as `open` or `protected`; SlideTalk does not store plaintext room passwords and the CLI does not reveal them. `slidetalk rm` removes the selected room's uploaded slide/audio references and deletes only physical files that no other room still references.

The browser hashes files before upload, extracts supported audio title/duration/cover metadata, the server stores files by SHA-256 and validated extension, and cleanup runs hourly. Room moderators can edit a track's display title and uploader display name; uploaders can edit their own track title.

Room moderators can create 24-hour migration links from room settings. A migration ID is a bearer secret that is shown once to the issuing browser, lets the holder join that room even when it has a password, and is stored in SQLite only as a SHA-256 hash.

Room links opened by browsers without a saved display name show a focused name gate at the same URL. After the visitor saves a name, SlideTalk opens the linked room automatically and uses the room title as the browser tab title while the room is active.

## Room Shortcuts

Active rooms use a room-first workspace: the slide, markdown, or audio stage takes most of the viewport, the top chrome is reduced to a compact timer row with icon-only timer, copy-link, and hand controls, and room controls live in a collapsible side rail. Participant and observer rail sections show online/total counts and list online members before offline members. The starting page profile, create-room, and join-room controls are hidden while a room is open.

Keyboard shortcuts are ignored while typing in form fields. Press `?` in a room to open the shortcuts panel and change local key bindings. Defaults:

- `b`: previous speaker, moderators only
- `n`: next speaker, moderators only
- `p`: start or pause timer, moderators only
- `t`: reset and start timer, moderators only
- `[`: previous slide, moderators only
- `]`: next slide, moderators only
- `?`: show or hide shortcut configuration

Security notes live in [docs/security.md](docs/security.md).

## Planning

Seed milestone plans live in [plans/seed/000-index.md](plans/seed/000-index.md). A fresh context can continue the project with:

```text
Implement the next milestone.
```
