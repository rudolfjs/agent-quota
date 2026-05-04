#!/bin/sh
set -eu

# ── Configuration ─────────────────────────────────────────────────────────────

OWNER_REPO="${OWNER_REPO:-rudolfjs/agent-quota}"
BINARY="agent-quota"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"
YES="${YES:-}"

# ── Colors & symbols ─────────────────────────────────────────────────────────

if [ -t 1 ]; then
  BOLD='\033[1m'
  DIM='\033[2m'
  CYAN='\033[36m'
  GREEN='\033[32m'
  YELLOW='\033[33m'
  RED='\033[31m'
  RESET='\033[0m'
  CHECK="${GREEN}✓${RESET}"
  ARROW="${CYAN}→${RESET}"
  WARN="${YELLOW}!${RESET}"
  CROSS="${RED}✗${RESET}"
else
  BOLD='' DIM='' CYAN='' GREEN='' YELLOW='' RED='' RESET=''
  CHECK="+" ARROW="->" WARN="!" CROSS="x"
fi

# ── Helpers ───────────────────────────────────────────────────────────────────

info()  { printf "  ${ARROW} %s\n" "$1"; }
ok()    { printf "  ${CHECK} %s\n" "$1"; }
warn()  { printf "  ${WARN} %s\n" "$1"; }
fail()  { printf "  ${CROSS} %s\n" "$1" >&2; exit 1; }

validate_inputs() {
  case "$OWNER_REPO" in
    */*)  ;;
    *) fail "invalid OWNER_REPO value: $OWNER_REPO (expected owner/repo format)" ;;
  esac
  case "$VERSION" in
    latest|v[0-9]*.[0-9]*.[0-9]*) ;;
    *) fail "invalid VERSION value: $VERSION (expected 'latest' or 'vX.Y.Z')" ;;
  esac
}

banner() {
  printf "\n"
  printf "  ${BOLD}agent-quota installer${RESET}\n"
  printf "  ${DIM}https://github.com/%s${RESET}\n" "$OWNER_REPO"
  printf "\n"
}

# ── Core functions ────────────────────────────────────────────────────────────

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

  fail "either curl or wget is required"
}

detect_os() {
  case $(uname -s) in
    Linux)  echo linux ;;
    Darwin) echo darwin ;;
    *)      fail "unsupported operating system: $(uname -s) (Linux and macOS supported; on Windows use WSL2)" ;;
  esac
}

detect_arch() {
  case $(uname -m) in
    x86_64|amd64)   echo amd64 ;;
    arm64|aarch64)  echo arm64 ;;
    *)              fail "unsupported architecture: $(uname -m)" ;;
  esac
}

validate_target() {
  case "$1/$2" in
    linux/amd64|darwin/amd64|darwin/arm64) ;;
    *) fail "unsupported platform: $(pretty_os "$1") / $(pretty_arch "$2") (supported: Linux x86_64, macOS Intel, macOS Apple Silicon; on Windows use WSL2)" ;;
  esac
}

pretty_os() {
  case $1 in
    darwin) printf "macOS" ;;
    linux)  printf "Linux" ;;
    *)      printf "%s" "$1" ;;
  esac
}

pretty_arch() {
  case $1 in
    amd64) printf "x86_64 (Intel/AMD)" ;;
    arm64) printf "arm64 (Apple Silicon/aarch64)" ;;
    *)     printf "%s" "$1" ;;
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
      grep "  $base$" "$(basename "$checksums")" | sha256sum -c - >/dev/null 2>&1
    )
    return $?
  fi

  if command -v shasum >/dev/null 2>&1; then
    expected=$(awk -v name="$base" '$2 == name { print $1 }' "$checksums")
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
    if [ "$expected" != "$actual" ]; then
      return 1
    fi
    return 0
  fi

  warn "no sha256 checksum tool found — skipping verification"
  return 0
}

confirm() {
  if [ -n "$YES" ]; then
    return 0
  fi

  printf "\n"
  printf "  ${BOLD}Ready to install${RESET}\n"
  printf "\n"
  printf "    binary:   ${BOLD}%s${RESET} %s\n" "$BINARY" "$VERSION"
  printf "    platform: %s / %s\n" "$(pretty_os "$os")" "$(pretty_arch "$arch")"
  printf "    location: ${BOLD}%s/%s${RESET}\n" "$BIN_DIR" "$BINARY"
  printf "\n"

  if [ -f "$BIN_DIR/$BINARY" ]; then
    warn "this will overwrite the existing binary at $BIN_DIR/$BINARY"
    printf "\n"
  fi

  printf "  ${BOLD}Proceed with installation?${RESET} [Y/n] "
  if [ -e /dev/tty ]; then
    read -r reply </dev/tty
  elif [ -t 0 ]; then
    read -r reply
  else
    printf "\n"
    fail "no TTY available for confirmation — set YES=1 to skip the prompt"
  fi
  case $reply in
    [Nn]*)
      printf "\n"
      info "no changes made — maybe next time!"
      printf "\n"
      exit 0
      ;;
  esac
}

# ── Main ──────────────────────────────────────────────────────────────────────

main() {
  validate_inputs
  banner

  # Detect platform
  info "detecting platform..."
  os=$(detect_os)
  arch=$(detect_arch)
  validate_target "$os" "$arch"
  ok "$(pretty_os "$os") / $(pretty_arch "$arch")"

  # Resolve version
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT INT TERM

  if [ "$VERSION" = "latest" ]; then
    info "looking up latest release..."
    download "https://api.github.com/repos/$OWNER_REPO/releases/latest" "$tmpdir/release.json"
    VERSION=$(latest_tag "$tmpdir/release.json")
    if [ -z "$VERSION" ]; then
      fail "could not determine latest release version"
    fi
  fi
  ok "version $VERSION"

  # Confirm before doing anything
  confirm

  printf "\n"

  # Download archive
  version_no_v=${VERSION#v}
  archive="${BINARY}_${version_no_v}_${os}_${arch}.tar.gz"
  base_url="https://github.com/$OWNER_REPO/releases/download/$VERSION"

  info "downloading ${archive}..."
  download "$base_url/$archive" "$tmpdir/$archive"
  ok "downloaded"

  # Verify checksum
  info "verifying checksum..."
  download "$base_url/checksums.txt" "$tmpdir/checksums.txt"
  if verify_checksum "$tmpdir/$archive" "$tmpdir/checksums.txt"; then
    ok "checksum verified"
  else
    fail "checksum verification failed — aborting"
  fi

  # Extract
  info "extracting..."
  # cd into tmpdir and pass a relative archive path: some tar implementations
  # (bsdtar/libarchive, busybox) misinterpret absolute -f paths when combined
  # with -C, producing "Cannot open" errors with a duplicated path prefix.
  (cd -- "$tmpdir" && tar -xzf "$archive")
  binary_path=$(find "$tmpdir" -type f -name "$BINARY" | head -n 1)
  if [ -z "$binary_path" ]; then
    fail "could not locate binary in archive"
  fi
  ok "extracted"

  # Install
  info "installing to ${BIN_DIR}/${BINARY}..."
  mkdir -p "$BIN_DIR"
  install -m 0755 "$binary_path" "$BIN_DIR/$BINARY"
  ok "installed"

  # Symlink aq -> agent-quota
  ln -sf "$BIN_DIR/$BINARY" "$BIN_DIR/aq"
  ok "symlinked aq → $BINARY"

  # Done
  printf "\n"
  printf "  ${GREEN}${BOLD}Done!${RESET} Run ${BOLD}%s --help${RESET} (or ${BOLD}aq --help${RESET}) to get started.\n" "$BINARY"
  printf "\n"

  # Check PATH
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *)
      warn "${BIN_DIR} is not in your PATH"
      info "add it with:  export PATH=\"${BIN_DIR}:\$PATH\""
      printf "\n"
      ;;
  esac
}

main "$@"
