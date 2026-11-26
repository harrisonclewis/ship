#!/bin/sh
# Install ship from GitHub releases on Linux or macOS (x64 or arm64).
#
#   curl -fsSL https://raw.githubusercontent.com/harrisonclewis/ship/main/install.sh | bash
#
# Set SHIP_VERSION=0.2.0 to install a specific release, or SHIP_INSTALL_DIR to
# choose where the binary goes (default: ~/.local/bin).
set -eu

repo="harrisonclewis/ship"

tmpdir=""
install_tmp=""
cleanup() {
  if [ -n "$tmpdir" ]; then
    rm -rf "$tmpdir"
  fi
  if [ -n "$install_tmp" ]; then
    rm -f "$install_tmp"
  fi
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'ship install: %s\n' "$1" >&2
  exit 1
}

download() {
  url=$1
  dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    fail "curl or wget is required."
  fi
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1"
  else
    fail "sha256sum or shasum is required."
  fi
}

case "$(uname -s)" in
  Linux) platform="linux" ;;
  Darwin) platform="darwin" ;;
  *) fail "only Linux and macOS are supported." ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="x64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "unsupported architecture $(uname -m); use x64 or arm64." ;;
esac

asset="ship-$platform-$arch"
if [ -n "${SHIP_VERSION:-}" ]; then
  version=${SHIP_VERSION#v}
  [ -n "$version" ] || fail "SHIP_VERSION is empty."
  base="https://github.com/$repo/releases/download/v$version"
else
  base="https://github.com/$repo/releases/latest/download"
fi

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/ship-install.XXXXXX") || fail "could not create a temp directory."
printf 'Downloading %s...\n' "$asset"
download "$base/$asset" "$tmpdir/$asset"
download "$base/SHA256SUMS" "$tmpdir/SHA256SUMS"

expected=$(awk -v name="$asset" '{ file=$2; sub(/^\*/, "", file); if (file == name) { print $1; exit } }' "$tmpdir/SHA256SUMS")
[ -n "$expected" ] || fail "SHA256SUMS does not list $asset."
actual=$(hash_file "$tmpdir/$asset" | awk '{ print $1 }')
[ "$actual" = "$expected" ] || fail "checksum mismatch; not installing."

install_dir=${SHIP_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir" || fail "could not create $install_dir."

install_tmp="$install_dir/.ship.$$"
cp "$tmpdir/$asset" "$install_tmp"
chmod 0755 "$install_tmp"
mv -f "$install_tmp" "$install_dir/ship"
install_tmp=""
printf 'Installed ship to %s\n' "$install_dir/ship"

case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'warning: %s is not on PATH; add it to PATH to run ship.\n' "$install_dir" >&2 ;;
esac
