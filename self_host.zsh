#!/usr/bin/env zsh

set -euo pipefail

ROOT_DIR=${0:A:h}
CADDYFILE="$HOME/Caddyfile"
DEFAULT_URL="https://slidetalk.pinky.lilf.ir"
NODE_VERSION="${SLIDETALK_NODE_VERSION:-20}"
PROD_SESSION="${SLIDETALK_PROD_SESSION:-slidetalk}"
DEV_API_SESSION="${SLIDETALK_DEV_API_SESSION:-slidetalk-dev-api}"
DEV_WEB_SESSION="${SLIDETALK_DEV_WEB_SESSION:-slidetalk-dev-web}"
GO_ADDR="${SLIDETALK_ADDR:-127.0.0.1:8097}"
GO_PORT="${GO_ADDR##*:}"
VITE_HOST="${SLIDETALK_VITE_HOST:-127.0.0.1}"
VITE_PORT="${SLIDETALK_VITE_PORT:-5173}"
CADDY_BEGIN="# BEGIN SLIDETALK"
CADDY_END="# END SLIDETALK"

tmuxnew () {
    tmux kill-session -t "$1" &> /dev/null || true
    tmux new -d -s "$@"
}

usage() {
	cat <<'EOF'
Usage:
  ./self_host.zsh setup [url]
  ./self_host.zsh redeploy [url]
  ./self_host.zsh start [url]
  ./self_host.zsh stop
  ./self_host.zsh dev-start [url]

Default URL: https://slidetalk.pinky.lilf.ir
EOF
}

die() {
	print -u2 -- "Error: $*"
	exit 1
}

normalize_url() {
	local raw="${1:-$DEFAULT_URL}"
	raw="${raw%/}"
	if [[ "$raw" != http://* && "$raw" != https://* ]]; then
		raw="https://$raw"
	fi
	print -- "$raw"
}

url_scheme() {
	print -- "${1%%://*}"
}

url_host() {
	local without_scheme="${1#*://}"
	print -- "${without_scheme%%/*}"
}

require_base_commands() {
	local cmd
	for cmd in go tmux caddy zsh ss; do
		command -v "$cmd" >/dev/null 2>&1 || die "Missing required command: $cmd"
	done
	zsh -lc "nvm-load >/dev/null 2>&1 && nvm use ${NODE_VERSION} >/dev/null && command -v pnpm >/dev/null" </dev/null \
		|| die "Unable to load Node ${NODE_VERSION} with nvm-load and pnpm."
}

run_node_task() {
	local task="$1"
	local quoted_root=${(q)ROOT_DIR}
	zsh -lc "set -e; nvm-load >/dev/null; nvm use ${NODE_VERSION} >/dev/null; cd ${quoted_root}; ${task}" </dev/null
}

port_in_use() {
	local port="$1"
	ss -ltn | awk '{print $4}' | grep -Eq "(^|:)$port\$"
}

require_port_free() {
	local port="$1"
	local label="$2"
	if port_in_use "$port"; then
		die "$label port $port is already in use. Stop that process or set the related SLIDETALK_* override and retry."
	fi
}

tmux_env_args() {
	local names=(
		ALL_PROXY all_proxy
		HTTP_PROXY http_proxy
		HTTPS_PROXY https_proxy
		NO_PROXY no_proxy
		npm_config_proxy npm_config_https_proxy npm_config_noproxy
	)
	local name
	for name in "${names[@]}"; do
		if (( ${+parameters[$name]} )); then
			print -- "-e"
			print -- "${name}=${(P)name}"
		fi
	done
}

stop_session() {
	local session="$1"
	if tmux has-session -t "=${session}" 2>/dev/null; then
		tmux kill-session -t "=${session}"
		print -- "Stopped tmux session: $session"
	fi
}

stop_all_sessions() {
	stop_session "$PROD_SESSION"
	stop_session "$DEV_API_SESSION"
	stop_session "$DEV_WEB_SESSION"
}

wait_for_port_release() {
	local port="$1"
	local label="$2"
	local attempt
	for attempt in {1..15}; do
		if ! port_in_use "$port"; then
			return
		fi
		sleep 1
	done
	require_port_free "$port" "$label"
}

install_dependencies() {
	print -- "Installing frontend dependencies..."
	run_node_task "pnpm --dir web install --frozen-lockfile"
}

build_assets() {
	print -- "Building frontend assets..."
	run_node_task "pnpm --dir web build"
}

prod_build_exists() {
	[[ -f "$ROOT_DIR/web/dist/index.html" ]]
}

ensure_prod_build() {
	if ! prod_build_exists; then
		build_assets
	fi
}

write_caddyfile() {
	local public_url="$1"
	local mode="$2"
	local scheme host opposite_scheme tmp_file
	scheme=$(url_scheme "$public_url")
	host=$(url_host "$public_url")
	[[ "$scheme" == "http" || "$scheme" == "https" ]] || die "URL must use http or https."
	[[ -n "$host" ]] || die "URL host cannot be empty."
	if [[ "$scheme" == "https" ]]; then
		opposite_scheme="http"
	else
		opposite_scheme="https"
	fi

	tmp_file=$(mktemp)
	if [[ -f "$CADDYFILE" ]]; then
		local skipping=0
		while IFS= read -r line || [[ -n "$line" ]]; do
			if [[ "$line" == "$CADDY_BEGIN" ]]; then
				skipping=1
				continue
			fi
			if [[ "$line" == "$CADDY_END" ]]; then
				skipping=0
				continue
			fi
			(( skipping == 0 )) && print -r -- "$line" >> "$tmp_file"
		done < "$CADDYFILE"
	fi
	if [[ -s "$tmp_file" ]]; then
		print >> "$tmp_file"
	fi

	if [[ "$mode" == "prod" ]]; then
		cat >> "$tmp_file" <<EOF
$CADDY_BEGIN
${scheme}://${host} {
	encode zstd gzip
	handle /api/ws* {
		reverse_proxy ${GO_ADDR}
	}
	handle /api/* {
		reverse_proxy ${GO_ADDR}
	}
	handle /healthz {
		reverse_proxy ${GO_ADDR}
	}
	handle {
		root * ${ROOT_DIR}/web/dist
		try_files {path} /index.html
		file_server
	}
}

${opposite_scheme}://${host} {
	redir ${scheme}://${host}{uri} permanent
}
$CADDY_END
EOF
	else
		cat >> "$tmp_file" <<EOF
$CADDY_BEGIN
${scheme}://${host} {
	encode zstd gzip
	handle /api/ws* {
		reverse_proxy ${GO_ADDR}
	}
	handle /api/* {
		reverse_proxy ${GO_ADDR}
	}
	handle /healthz {
		reverse_proxy ${GO_ADDR}
	}
	handle {
		reverse_proxy ${VITE_HOST}:${VITE_PORT}
	}
}

${opposite_scheme}://${host} {
	redir ${scheme}://${host}{uri} permanent
}
$CADDY_END
EOF
	fi

	cp "$CADDYFILE" "${CADDYFILE}.slidetalk.bak" 2>/dev/null || true
	mv "$tmp_file" "$CADDYFILE"
	if ! caddy validate --config "$CADDYFILE" --adapter caddyfile >/dev/null; then
		if [[ -f "${CADDYFILE}.slidetalk.bak" ]]; then
			mv "${CADDYFILE}.slidetalk.bak" "$CADDYFILE"
		fi
		die "Caddyfile validation failed; restored the previous $CADDYFILE"
	fi
	if pgrep -x caddy >/dev/null 2>&1; then
		caddy reload --config "$CADDYFILE" --adapter caddyfile
	else
		print -- "Caddy is not running. Start it with: caddy run --config $CADDYFILE --adapter caddyfile"
	fi
}

start_prod() {
	local public_url="$1"
	local -a env_args
	env_args=(${(@f)$(tmux_env_args)})
	stop_all_sessions
	wait_for_port_release "$GO_PORT" "Go API"
	ensure_prod_build
	write_caddyfile "$public_url" prod
	tmuxnew "$PROD_SESSION" "${env_args[@]}" zsh -lc "cd ${(q)ROOT_DIR}; export SLIDETALK_ADDR=${(q)GO_ADDR} SLIDETALK_PUBLIC_URL=${(q)public_url}; exec go run ./cmd/slidetalk"
	print -- "Started production session: $PROD_SESSION"
	print -- "SlideTalk is available through Caddy at $public_url"
}

start_dev() {
	local public_url="$1"
	local -a env_args
	env_args=(${(@f)$(tmux_env_args)})
	stop_all_sessions
	wait_for_port_release "$GO_PORT" "Go API"
	wait_for_port_release "$VITE_PORT" "Vite"
	write_caddyfile "$public_url" dev
	tmuxnew "$DEV_API_SESSION" "${env_args[@]}" zsh -lc "cd ${(q)ROOT_DIR}; export SLIDETALK_ADDR=${(q)GO_ADDR} SLIDETALK_PUBLIC_URL=${(q)public_url} SLIDETALK_DEV=1; exec go run ./cmd/slidetalk"
	tmuxnew "$DEV_WEB_SESSION" "${env_args[@]}" zsh -lc "nvm-load >/dev/null; nvm use ${NODE_VERSION} >/dev/null; cd ${(q)ROOT_DIR}/web; exec pnpm dev --host ${VITE_HOST} --port ${VITE_PORT}"
	print -- "Started dev sessions: $DEV_API_SESSION, $DEV_WEB_SESSION"
	print -- "SlideTalk dev mode is available through Caddy at $public_url"
}

setup() {
	local public_url="$1"
	stop_all_sessions
	install_dependencies
	build_assets
	start_prod "$public_url"
}

redeploy() {
	local public_url="$1"
	stop_all_sessions
	if git -C "$ROOT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
		git -C "$ROOT_DIR" pull --ff-only
	fi
	install_dependencies
	build_assets
	start_prod "$public_url"
}

main() {
	local command="${1:-}"
	local public_url
	public_url=$(normalize_url "${2:-$DEFAULT_URL}")

	case "$command" in
		setup)
			require_base_commands
			setup "$public_url"
			;;
		redeploy)
			require_base_commands
			redeploy "$public_url"
			;;
		start)
			require_base_commands
			start_prod "$public_url"
			;;
		stop)
			command -v tmux >/dev/null 2>&1 || die "Missing required command: tmux"
			stop_all_sessions
			;;
		dev-start)
			require_base_commands
			start_dev "$public_url"
			;;
		""|-h|--help|help)
			usage
			;;
		*)
			usage
			die "Unknown command: $command"
			;;
	esac
}

main "$@"
