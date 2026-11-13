#!/bin/bash
# Provision the hs binary. Three modes, because the callers ask three
# different questions:
#
#   --in-tree  Guarantee THIS checkout carries bin/hs. Used by the
#              herdr-plugin.toml [[build]] hook, which runs on every
#              `herdr plugin install` and on `herdr plugin update`
#              against a fresh checkout that never carries the gitignored
#              binary.
#
#   --build    Always compile from source, and fail hard. Used by `make hs`,
#              where falling back to a prebuilt release would mask a broken
#              tree instead of reporting it.
#
#   (default)  Ensure hs is resolvable: $HS_BIN -> sibling bin/hs -> PATH.
#
# Build-or-download modes compile with the local Go toolchain first and fall
# back to a prebuilt release binary. The pattern and the pitfall it encodes
# (an unsatisfied --in-tree that looks satisfied) come from herdr-agent-usage
# (senna-lang/herdr-agent-usage issue #48) and are adapted unchanged here.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

REPO="WilberC/herdr-suite"
# The manifest is the source of truth for a checkout's version: locally built
# binaries intentionally do not receive release ldflags.
VERSION="$(awk -F '"' '/^version = / { print $2; exit }' "$ROOT/herdr-plugin.toml")"

# Normalize uname output to the Go GOOS/GOARCH names used by release assets
# (.github/workflows/release.yml): hs-<os>-<arch>.
release_asset_name() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) return 1 ;;
  esac
  case "$(uname -m)" in
    x86_64) arch="amd64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) return 1 ;;
  esac
  echo "hs-${os}-${arch}"
}

# Fetch $asset from release $tag ("" = latest) into $tmp, leaving no partial
# file behind on failure. gh works for private and public repos (it uses the
# user's auth); curl covers public repos without gh installed.
fetch_release_asset() {
  local tag="$1" asset="$2" tmp="$3"
  local gh_args=(release download) curl_path
  if [[ -n "$tag" ]]; then
    gh_args+=("$tag")
    curl_path="download/$tag"
  else
    curl_path="latest/download"
  fi
  gh_args+=(--repo "$REPO" --pattern "$asset" -O "$tmp")

  if command -v gh >/dev/null 2>&1; then
    gh "${gh_args[@]}" 2>/dev/null || rm -f "$tmp"
  fi
  if [[ ! -s "$tmp" ]] && command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://github.com/$REPO/releases/$curl_path/$asset" -o "$tmp" || rm -f "$tmp"
  fi
  [[ -s "$tmp" ]]
}

# Install a prebuilt binary into bin/hs. Prefers the release matching this
# checkout's manifest version — `herdr plugin install --ref` can install
# source that is older or newer than the latest release — and falls back to
# latest when that tag carries no assets (an unreleased main, say).
download_release_binary() {
  local asset dest tmp
  asset="$(release_asset_name)" || return 1
  dest="$ROOT/bin/hs"
  tmp="$dest.download"
  rm -f "$tmp"

  echo "hs: downloading prebuilt binary ($asset)..." >&2
  # The version-matched attempt is speculative — a 404 here just means this
  # checkout is ahead of the last release — so keep its noise off stderr.
  if [[ -n "$VERSION" ]]; then
    fetch_release_asset "v$VERSION" "$asset" "$tmp" 2>/dev/null || true
  fi
  if [[ ! -s "$tmp" ]] && ! fetch_release_asset "" "$asset" "$tmp"; then
    return 1
  fi

  chmod +x "$tmp"
  # Sanity check before installing: the binary must run on this machine, and it
  # must answer "version". Both success shapes are accepted — "hs version
  # <release>" and "hs version dev" — because the check verifies that the asset
  # runs, not which release it is; an asset for another platform, a truncated
  # download or an unrelated file exits non-zero or prints something else, and
  # nothing is installed in that case.
  local sanity
  sanity="$("$tmp" version 2>/dev/null)" || { rm -f "$tmp"; return 1; }
  case "$sanity" in
    "hs version "*) ;;
    *) rm -f "$tmp"; return 1 ;;
  esac
  mv -f "$tmp" "$dest"
}

build_from_source() {
  command -v go >/dev/null 2>&1 || return 1
  echo "hs: building bin/hs (go build)..." >&2
  mkdir -p "$ROOT/bin"
  (cd "$ROOT" && go build -o bin/hs ./cmd/hs)
}

# Guarantee $ROOT/bin/hs exists, by any available means. A source build
# failing (cold module cache, no network, no toolchain) is not fatal on its
# own — the prebuilt download is a real second chance.
install_binary() {
  if build_from_source; then
    return 0
  fi
  if command -v go >/dev/null 2>&1; then
    echo "hs: go build failed; trying the prebuilt release binary..." >&2
  fi
  if download_release_binary; then
    echo "hs: prebuilt binary installed to bin/hs" >&2
    return 0
  fi
  echo "hs: could not build or download the hs binary." >&2
  echo "Fix one of:" >&2
  echo "  - install Go (https://go.dev/dl/), then run: make hs  (in the plugin root)" >&2
  echo "  - install gh (https://cli.github.com/) and authenticate, then re-install the plugin" >&2
  return 127
}

# True when hs is already resolvable the way the plugin actions resolve it.
hs_resolvable() {
  [[ -n "${HS_BIN:-}" && -x "$HS_BIN" ]] && return 0
  [[ -x "$ROOT/bin/hs" ]] && return 0
  command -v hs >/dev/null 2>&1
}

case "${1:-}" in
  --in-tree)
    if [[ ! -x "$ROOT/bin/hs" ]]; then
      install_binary
    fi
    ;;
  --build)
    build_from_source || {
      echo "hs: go build failed (is the Go toolchain installed?)" >&2
      exit 1
    }
    ;;
  "")
    if ! hs_resolvable; then
      install_binary
    fi
    ;;
  *)
    echo "usage: ensure-binary.sh [--in-tree|--build]" >&2
    exit 2
    ;;
esac
