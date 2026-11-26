#!/usr/bin/env bash
# Generates the Ed25519 key pair that signs ship releases.
#
#   scripts/release-keygen.sh [private-key-path]
#
# Writes the private key (mode 0600) outside the repository, embeds the public
# key in internal/update/releasekey.go, and prints the command that stores the
# private key as the SHIP_RELEASE_SIGNING_KEY GitHub secret.
#
# Back the private key up (e.g. in a password manager). Installed copies of ship
# only accept updates signed by it; if it is lost, every user has to reinstall
# by hand after the key is replaced.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
key_path="${1:-$HOME/.config/ship-release/release-signing-key.pem}"
key_go="$repo_root/internal/update/releasekey.go"

command -v openssl >/dev/null 2>&1 || {
  echo "openssl is required." >&2
  exit 1
}
case "$key_path" in
  /*) ;;
  *) key_path="$PWD/$key_path" ;;
esac
key_dir="$(dirname "$key_path")"
if [ -d "$key_dir" ]; then
  key_dir="$(cd "$key_dir" && pwd)"
fi
case "$key_dir/" in
  "$repo_root"/*)
    echo "Refusing to write the private key inside the repository." >&2
    exit 1
    ;;
esac
if [ -e "$key_path" ]; then
  echo "$key_path already exists; refusing to overwrite it." >&2
  exit 1
fi

mkdir -p "$(dirname "$key_path")"
chmod 0700 "$(dirname "$key_path")"
(umask 077 && openssl genpkey -algorithm ed25519 -out "$key_path")
chmod 0600 "$key_path"
public_pem="$(openssl pkey -in "$key_path" -pubout)"

{
  cat <<'HEADER'
package update

// ReleasePublicKeyPEM is the Ed25519 public key (SPKI PEM) that signs ship
// release checksums. The matching private key lives only in the
// SHIP_RELEASE_SIGNING_KEY GitHub Actions secret. scripts/release-keygen.sh
// generates the pair and rewrites this value. While it is empty, builds cannot
// verify signatures, so cannot install updates.
HEADER
  # shellcheck disable=SC2016 # backticks are Go raw-string delimiters
  printf 'const ReleasePublicKeyPEM = `%s\n`\n' "$public_pem"
} > "$key_go"

cat <<EOF2
Private key: $key_path (back this up; never commit it)
Public key written to: ${key_go#"$repo_root"/}

Store the private key as an environment secret for the release workflow:
  gh secret set SHIP_RELEASE_SIGNING_KEY --env release --repo harrisonclewis/ship < "$key_path"
EOF2
