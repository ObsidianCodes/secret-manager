#!/bin/sh
# secretman installer.
#
#   curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
#
# Repo is private, so raw.githubusercontent and the release assets both need a
# token. Script take it from GITHUB_TOKEN, GH_TOKEN, or `gh auth token`.
#
# Env knobs:
#   VERSION       tag to install, default latest release
#   INSTALL_DIR   where binary land, default $HOME/.local/bin
#   GITHUB_TOKEN  token for private repo access
set -eu

REPO="ObsidianCodes/secret-manager"
BIN="secretman"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
API="https://api.github.com"

say() { printf '%s\n' "$*" >&2; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "need $1 on PATH"; }

need curl
need tar
need uname

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

# ---------------------------------------------------------------- token

token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -z "$token" ] && command -v gh >/dev/null 2>&1; then
	token=$(gh auth token 2>/dev/null || true)
fi
[ -n "$token" ] || die "private repo: set GITHUB_TOKEN, or run 'gh auth login'"

api() {
	curl -fsSL \
		-H "Authorization: Bearer $token" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		"$@"
}

# ---------------------------------------------------------------- release

if [ "$VERSION" = latest ]; then
	rel_url="$API/repos/$REPO/releases/latest"
else
	rel_url="$API/repos/$REPO/releases/tags/$VERSION"
fi

rel=$(api "$rel_url") || die "no release at $rel_url (bad token, or none published yet)"

tag=$(printf '%s' "$rel" | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*: *"//; s/"$//')
[ -n "$tag" ] || die "could not read tag_name from release"

asset="${BIN}_${tag}_${os}_${arch}.tar.gz"

# Asset objects come back with "id" before "name"; splitting on { keeps the
# pair in one chunk, ahead of the nested uploader object.
asset_id=$(
	printf '%s' "$rel" | tr '{' '\n' |
		grep "\"name\": *\"$asset\"" |
		grep -o '"id": *[0-9]*' | head -1 |
		grep -o '[0-9]*'
) || true
[ -n "${asset_id:-}" ] || die "release $tag has no asset $asset"

sums_id=$(
	printf '%s' "$rel" | tr '{' '\n' |
		grep '"name": *"checksums.txt"' |
		grep -o '"id": *[0-9]*' | head -1 |
		grep -o '[0-9]*'
) || true

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "==> $BIN $tag ($os/$arch)"
api -H "Accept: application/octet-stream" \
	-o "$tmp/$asset" "$API/repos/$REPO/releases/assets/$asset_id" ||
	die "download failed"

# ---------------------------------------------------------------- checksum

if [ -n "${sums_id:-}" ]; then
	api -H "Accept: application/octet-stream" \
		-o "$tmp/checksums.txt" "$API/repos/$REPO/releases/assets/$sums_id" ||
		die "checksums download failed"

	want=$(grep " \*\{0,1\}$asset\$" "$tmp/checksums.txt" | awk '{print $1}' | head -1)
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
	else
		got=""
		say "warn: no sha256 tool, skipping checksum"
	fi
	if [ -n "$got" ]; then
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

mkdir -p "$INSTALL_DIR"
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
