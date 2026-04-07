#!/bin/sh
set -eu

# ── Configuration ─────────────────────────────────────────────────────────────

MIN_GO_VERSION="1.25.0"

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

# ── Core functions ────────────────────────────────────────────────────────────

check_go() {
  if ! command -v go >/dev/null 2>&1; then
    fail "Go is not installed. Install Go ${MIN_GO_VERSION}+ from https://go.dev/dl/"
  fi

  go_version_raw=$(go version)
  go_ver=$(printf '%s' "$go_version_raw" | sed -n 's/.*go\([0-9][0-9]*\.[0-9][0-9]*\(\.[0-9][0-9]*\)\?\).*/\1/p')
  if [ -z "$go_ver" ]; then
    fail "could not parse Go version from: ${go_version_raw}"
  fi

  oldest=$(printf '%s\n%s' "$MIN_GO_VERSION" "$go_ver" | sort -V | head -n 1)
  if [ "$oldest" != "$MIN_GO_VERSION" ]; then
    fail "Go ${go_ver} is too old — requires Go ${MIN_GO_VERSION}+. Upgrade at https://go.dev/dl/"
  fi

  ok "Go ${go_ver} (>= ${MIN_GO_VERSION})"
}

check_gh() {
  if ! command -v gh >/dev/null 2>&1; then
    warn "gh (GitHub CLI) is not installed"
    info "install it from: https://cli.github.com/"
    info "  macOS:  brew install gh"
    info "  Linux:  see https://github.com/cli/cli/blob/trunk/docs/install_linux.md"
    return 1
  fi

  gh_ver=$(gh --version | head -n 1 | sed -n 's/.*version \([0-9][0-9.]*\).*/\1/p')
  ok "gh ${gh_ver:-unknown}"
}

install_tools() {
  info "installing lefthook..."
  go install github.com/evilmartians/lefthook/v2@latest
  ok "lefthook"

  info "installing changie..."
  go install github.com/miniscruff/changie@latest
  ok "changie"

  info "installing golangci-lint..."
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
  ok "golangci-lint"
}

check_gopath_bin() {
  gopath_bin="$(go env GOPATH)/bin"
  case ":${PATH}:" in
    *":${gopath_bin}:"*)
      ok "${gopath_bin} is on PATH"
      ;;
    *)
      warn "${gopath_bin} is NOT on your PATH"
      info "add it with:  export PATH=\"\$(go env GOPATH)/bin:\$PATH\""
      info "or add that line to your ~/.bashrc / ~/.zshrc"
      ;;
  esac
}

download_modules() {
  info "downloading Go module dependencies..."
  go mod download
  ok "modules downloaded"
}

install_hooks() {
  info "installing git hooks via lefthook..."
  lefthook install
  ok "git hooks installed"
}

# ── Main ──────────────────────────────────────────────────────────────────────

main() {
  printf "\n"
  printf "  ${BOLD}agent-quota dev environment setup${RESET}\n"
  printf "\n"

  info "checking prerequisites..."
  check_go
  check_gh || true

  printf "\n"
  info "installing Go development tools..."
  install_tools

  printf "\n"
  check_gopath_bin

  printf "\n"
  download_modules

  printf "\n"
  install_hooks

  printf "\n"
  printf "  ${GREEN}${BOLD}Done!${RESET} Your development environment is ready.\n"
  printf "  Run ${BOLD}make build${RESET} to verify everything works.\n"
  printf "\n"
}

main "$@"
