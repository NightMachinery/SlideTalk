# Self Hosting

SlideTalk can run on a VPS without Docker by using the Go server, a Vite production build, tmux, and Caddy.

## Prerequisites

- Go 1.22 or newer.
- zsh with `nvm-load` available to non-interactive login shells.
- Node 20 through `nvm-load` and `nvm use 20`.
- pnpm.
- tmux.
- Caddy using `~/Caddyfile`.
- Ports `8097` and, for development mode, `5173` available on localhost.

The script checks for `go`, `pnpm` after loading Node, `tmux`, `caddy`, `zsh`, and `ss`.

## Commands

```bash
./self_host.zsh setup [url]
./self_host.zsh redeploy [url]
./self_host.zsh start [url]
./self_host.zsh stop
./self_host.zsh dev-start [url]
```

The default URL is `https://slidetalk.pinky.lilf.ir`. If a URL has no scheme, the script treats it as HTTPS. Use an explicit `http://` URL for plain HTTP hosting.

## First Setup

```bash
./self_host.zsh setup https://talk.example.com
```

`setup` stops existing SlideTalk tmux sessions, installs frontend dependencies with pnpm, builds `web/dist`, installs the `slidetalk` CLI to `~/.local/bin`, writes the managed Caddy block, reloads Caddy when it is running, and starts the Go server in tmux.

The production tmux session is named `slidetalk` by default.

## Redeploy

```bash
./self_host.zsh redeploy https://talk.example.com
```

`redeploy` stops SlideTalk sessions, runs `git pull --ff-only` when the directory is a Git worktree, reinstalls frontend dependencies with a frozen pnpm lockfile, rebuilds assets, reinstalls the `slidetalk` CLI, updates Caddy, and starts production again.

## Start And Stop

```bash
./self_host.zsh start https://talk.example.com
./self_host.zsh stop
```

`start` stops production and development sessions, builds `web/dist` if it is missing, writes the production Caddy block, and starts the Go server. `stop` stops the managed tmux sessions and leaves the Caddy block intact.

## Development Mode

```bash
./self_host.zsh dev-start https://talk.example.com
```

`dev-start` stops production and development sessions, writes a development Caddy block, starts the Go server with `SLIDETALK_DEV=1`, and starts Vite with hot reload. The managed sessions are:

- `slidetalk-dev-api`
- `slidetalk-dev-web`

## Data Paths

The server stores runtime data under `~/.slidetalk` by default:

- Database: `~/.slidetalk/slidetalk.db`
- Admin bootstrap token: `~/.slidetalk/admin_token`
- Slides: `~/.slidetalk/slides/`
- Audio: `~/.slidetalk/audio/`
- Public URL marker for CLI links: `~/.slidetalk/public_url`

Set `SLIDETALK_DATA_DIR` before running the script to use another data directory. Set `SLIDETALK_ADDR` to change the Go listen address; the default is `127.0.0.1:8097`.

## Caddy Behavior

The script replaces only the block between these markers in `~/Caddyfile`:

```text
# BEGIN SLIDETALK
# END SLIDETALK
```

Production mode serves static files from `web/dist` through Caddy and reverse proxies `/api/*`, `/api/ws*`, and `/healthz` to the Go server.

Development mode reverse proxies app traffic to Vite and proxies `/api/*`, `/api/ws*`, and `/healthz` to the Go server.

For HTTPS URLs, the script writes an explicit HTTP-to-HTTPS redirect block. For HTTP URLs, it writes an explicit HTTPS-to-HTTP redirect block.

## HTTP Caveat

Browser clipboard and other secure-context APIs may be unavailable on plain HTTP except on localhost. SlideTalk still works over HTTP, but browser features that require HTTPS can need manual copy/paste fallback behavior.

The room-link copy control uses `navigator.clipboard` when the browser allows it. When clipboard access is blocked, the app shows a selected text field with the room link so users can copy manually.

## Security Headers And Limits

The Go server adds security headers to responses, including `nosniff`, `no-referrer`, a valid Permissions Policy, and a Content Security Policy that allows same-origin app assets, blob workers for PDF rendering, and WebSocket connections. The generated Caddy block replaces stale `Permissions-Policy` values so older `browsing-topics` entries do not trigger browser console warnings.

JSON request bodies are capped by the server. PDF, PNG, JPEG, WebP, and GIF slide uploads are capped by `SLIDETALK_SLIDE_UPLOAD_LIMIT`, which defaults to 200 MiB.

Audio uploads are capped for non-admin users by `SLIDETALK_AUDIO_FILE_UPLOAD_LIMIT`, which defaults to 50 MiB. Site admins can upload larger audio files after a browser confirmation. Supported audio formats include MP3, M4A, AAC, Ogg/Opus, WAV, FLAC, and WebM audio. The browser skips clearly unsupported selections before upload to avoid wasting bandwidth, and the server still validates uploaded content. Slide and audio uploads are rejected when retaining the upload would leave less free disk space than `SLIDETALK_MIN_FREE_SPACE`, which defaults to 0.5 GiB. Browsers check upload capacity before sending audio bytes and stop queued uploads when the server reports insufficient free space. Room uploaded files are cleaned after the room survival deadline, which defaults to `SLIDETALK_ROOM_GC_AFTER=7d`. Room moderators can prolong room survival up to 10 days from now, while site admins who are room members can set survival at least 24 hours out or choose never expire. Never-expire rooms are excluded from cleanup. `SLIDETALK_AUDIO_FILES_GC_AFTER` remains as a deprecated compatibility setting, but room survival controls audio cleanup.

Browsers keep downloaded audio blobs in IndexedDB so playback can resume from cached files after refresh. That browser cache is local to each client and self-prunes entries older than 30 days, then trims oldest cached audio beyond 2 GiB or 500 files. Browsers stream uncached shared audio immediately, cache the active track in the background, and also start caching the server-announced next track so playback can advance with less delay. Shuffle mode uses a deterministic per-room order so clients can cache the actual upcoming track. Moderators can use Restore cached audio in the Cache panel to upload cached audio blobs from that browser profile into the currently open room, skipping cached files whose SHA-256 hash is already present in the room; it cannot recover deleted server-side audio unless the blobs remain in that browser cache. Cache entries created with uploader metadata restore the original uploader display name, while older cache entries restore without a displayed uploader. Browsers also keep local mute and volume preferences in localStorage per room member, so one room member's playback preference does not affect another room or user in the same browser. Server-side audio download links include durable room-track bearer tokens for external download managers and remain valid until the track is removed.

## CLI

`setup` and `redeploy` install a local operator command:

```bash
slidetalk ls [--sort=size|created-date|creator-name|online-count]
slidetalk rm ROOM_ID... [-y]
```

`slidetalk ls` reads the configured SQLite database and prints room metadata, storage estimates, links, and password status. Passwords are shown only as `open` or `protected`; existing and future room passwords remain bcrypt-hashed and are not recoverable by the CLI.

`slidetalk rm` force-expires uploaded files for the selected rooms. Before deleting anything, it shows the room IDs, room file references that will be removed, physical files eligible for deletion, and bytes that would be freed. Physical files still referenced by other rooms are kept. Pass `-y` to skip the confirmation prompt.

Admin-token submissions, invalid room-password attempts, and WebSocket ticket creation are rate-limited.

## Proxy Environment

The script does not hardcode proxy values. When it launches tmux sessions, it preserves existing proxy-related environment variables using tmux `-e NAME=value` arguments. This includes common `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, lowercase variants, and pnpm/npm proxy variables when they are already set.

## Shell Behavior

The script runs Node and pnpm commands through non-interactive zsh login shells. This keeps `nvm-load` available without allowing deploy installs or builds to read from the terminal, which avoids suspended `redeploy` jobs in SSH or backgrounded shell contexts.

## Troubleshooting Occupied Ports

If startup reports that port `8097` or `5173` is occupied, find the process and stop it or choose a different port:

```bash
ss -ltnp | rg ':8097|:5173'
SLIDETALK_ADDR=127.0.0.1:18097 ./self_host.zsh start https://talk.example.com
SLIDETALK_VITE_PORT=15173 ./self_host.zsh dev-start https://talk.example.com
```

After changing `SLIDETALK_ADDR`, rerun `start` or `dev-start` so the Caddy block points at the same Go address.
