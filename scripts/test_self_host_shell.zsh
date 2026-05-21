#!/usr/bin/env zsh

set -euo pipefail

ROOT_DIR=${0:A:h:h}
SCRIPT="$ROOT_DIR/self_host.zsh"

zsh -n "$SCRIPT"

if grep -n -- 'zsh -i' "$SCRIPT"; then
	print -u2 -- "self_host.zsh must not use interactive zsh for automated deploy tasks."
	exit 1
fi

if ! grep -q -- 'Permissions-Policy "camera=(), microphone=(), geolocation=()"' "$SCRIPT"; then
	print -u2 -- "self_host.zsh must write a valid Permissions-Policy header without browsing-topics."
	exit 1
fi

if grep -q -- 'browsing-topics' "$SCRIPT"; then
	print -u2 -- "self_host.zsh must not emit the deprecated browsing-topics Permissions-Policy feature."
	exit 1
fi
