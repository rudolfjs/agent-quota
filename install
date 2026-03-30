#!/bin/sh
set -eu

OWNER_REPO="${OWNER_REPO:-schnetlerr/agent-quota}"
BINARY="agent-quota"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

download() {
  url=$1
  out=$2

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
    return 0
  fi

  echo "either curl or wget is required" >&2
  exit 1
}

detect_os() {
  case $(uname -s) in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    *)
      echo "unsupported operating system: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case $(uname -m) in
    x86_64|amd64) echo amd64 ;;
    arm64|aarch64) echo arm64 ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  response=$1
  sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$response" | head -n 1
}

verify_checksum() {
  archive=$1
  checksums=$2
  base=$(basename "$archive")

  if command -v sha256sum >/dev/null 2>&1; then
    (
      cd "$(dirname "$archive")"
      grep "  $base$" "$(basename "$checksums")" | sha256sum -c -
    )
    return 0
  fi

  if command -v shasum >/dev/null 2>&1; then
    expected=$(awk -v name="$base" '$2 == name { print $1 }' "$checksums")
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
    if [ "$expected" != "$actual" ]; then
      echo "checksum verification failed for $base" >&2
      exit 1
    fi
    return 0
  fi

  echo "warning: no sha256 checksum tool found; skipping verification" >&2
}

main() {
  os=$(detect_os)
  arch=$(detect_arch)
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT INT TERM

  if [ "$VERSION" = "latest" ]; then
    download "https://api.github.com/repos/$OWNER_REPO/releases/latest" "$tmpdir/release.json"
    VERSION=$(latest_tag "$tmpdir/release.json")
  fi

  if [ -z "$VERSION" ]; then
    echo "failed to resolve release version" >&2
    exit 1
  fi

  version_no_v=${VERSION#v}
  archive="${BINARY}_${version_no_v}_${os}_${arch}.tar.gz"
  base_url="https://github.com/$OWNER_REPO/releases/download/$VERSION"

  download "$base_url/$archive" "$tmpdir/$archive"
  download "$base_url/checksums.txt" "$tmpdir/checksums.txt"
  verify_checksum "$tmpdir/$archive" "$tmpdir/checksums.txt"

  tar -xzf "$tmpdir/$archive" -C "$tmpdir"
  binary_path=$(find "$tmpdir" -type f -name "$BINARY" | head -n 1)
  if [ -z "$binary_path" ]; then
    echo "failed to locate extracted binary" >&2
    exit 1
  fi

  mkdir -p "$BIN_DIR"
  install -m 0755 "$binary_path" "$BIN_DIR/$BINARY"

  echo "installed $BINARY $VERSION to $BIN_DIR/$BINARY"
}

main "$@"
