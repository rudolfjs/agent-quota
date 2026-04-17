#!/bin/sh
# End-to-end smoke test for agent-quota.
#
# Intent: catch regressions that make the built binary unrunnable, such as
# the self-update permission-denied bug this script was introduced alongside.
# The suite must run offline; anything that depends on a live provider API
# stays out of scope.
#
# Runs in the devcontainer (see .devcontainer/devcontainer.json) and in CI.
set -eu

BIN="${BIN:-/tmp/agent-quota-smoke}"
CMD="./cmd/agent-quota/"

# ── Helpers ───────────────────────────────────────────────────────────────────

info() { printf '  → %s\n' "$1"; }
ok()   { printf '  ✓ %s\n' "$1"; }
fail() { printf '  ✗ %s\n' "$1" >&2; exit 1; }

# ── Build ─────────────────────────────────────────────────────────────────────

info "building $BIN from $CMD"
go build -o "$BIN" "$CMD"
ok "build succeeded"

# ── Filesystem-level sanity ───────────────────────────────────────────────────
#
# The self-update bug landed a non-executable 0o600 file; this catches the
# most direct symptom before we ever try to exec the binary.

test -x "$BIN" || fail "built binary is not executable (mode regression?)"
ok "binary has executable bit"

# ── Subcommand wiring ─────────────────────────────────────────────────────────
#
# Offline-safe invocations that exercise cobra + fang and prove the binary
# actually executes user-facing code paths. If any of these fail the build
# is broken in a way no unit test will catch.

info "agent-quota --version"
"$BIN" --version >/dev/null
ok "--version runs"

info "agent-quota --help"
"$BIN" --help | grep -q 'USAGE' || fail "--help output is missing USAGE block"
"$BIN" --help | grep -q 'self-update' || fail "--help output is missing self-update subcommand"
ok "--help renders"

info "agent-quota status --help"
"$BIN" status --help | grep -q 'USAGE' || fail "status --help output is missing USAGE block"
ok "status --help renders"

info "agent-quota self-update --help"
"$BIN" self-update --help | grep -q 'USAGE' || fail "self-update --help output is missing USAGE block"
ok "self-update --help renders"

ok "smoke test passed"
