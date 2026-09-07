#!/bin/sh
# secretman installer.
#
#   curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
#
# Pull the release tarball for this os/arch, verify its sha256, drop the
# binary in place. No token, no API call, no dependency past curl and tar.
#
# Env knobs:
#   VERSION       tag to install, default latest release
#   INSTALL_DIR   where binary land, default $HOME/.local/bin
set -eu

REPO="ObsidianCodes/secret-manager"
BIN="secretman"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
DL="https://github.com/$REPO/releases/download"

say() { printf '%s\n' "$*" >&2; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "need $1 on PATH"; }

need curl
need tar
need uname
need awk

# ---------------------------------------------------------------- platform

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
	darwin | linux) ;;
	*) die "unsupported os: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported arch: $arch" ;;
esac

# ---------------------------------------------------------------- tag
#
# /releases/latest redirect to /releases/tag/<tag>. Cheaper than the API and
# not rate limited the same way.

tag="$VERSION"
if [ "$tag" = latest ]; then
	url=$(curl -fsSLo /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest") ||
		die "cannot reach github releases"
	tag=${url##*/}
fi
case "$tag" in
	v*) ;;
	*) die "no release tag found (got '$tag')" ;;
esac

asset="${BIN}_${tag}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "==> $BIN $tag ($os/$arch)"
curl -fsSL -o "$tmp/$asset" "$DL/$tag/$asset" ||
	die "no asset $asset in release $tag"

# ---------------------------------------------------------------- checksum

if curl -fsSL -o "$tmp/checksums.txt" "$DL/$tag/checksums.txt"; then
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
	else
		got=""
		say "warn: no sha256 tool, skipping checksum"
	fi

	if [ -n "$got" ]; then
		# Second field carry a leading * in binary mode; strip it before match.
		want=$(awk -v n="$asset" '
			{ sub(/^\*/, "", $2); if ($2 == n) { print $1; exit } }
		' "$tmp/checksums.txt")
		[ -n "$want" ] || die "checksums.txt name no $asset"
		[ "$want" = "$got" ] || die "checksum mismatch: want $want got $got"
		say "==> checksum ok"
	fi
else
	say "warn: release carry no checksums.txt"
fi

# ---------------------------------------------------------------- install

tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/$BIN" ] || die "archive hold no $BIN"

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
[ -d "$INSTALL_DIR" ] || die "cannot create $INSTALL_DIR"

if [ -w "$INSTALL_DIR" ]; then
	install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
else
	need sudo
	say "==> $INSTALL_DIR not writable, using sudo"
	sudo install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
fi

say "==> installed $INSTALL_DIR/$BIN"

case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) say "warn: $INSTALL_DIR not on PATH — add: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac

"$INSTALL_DIR/$BIN" --version 2>/dev/null || true
