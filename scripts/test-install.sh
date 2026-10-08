#!/bin/sh
# Offline installer tests: no network, real credentials or installation paths.
set -eu
root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
mkdir -p "$tmp/mock" "$tmp/assets/package"
printf '#!/bin/sh\necho fixture\n' > "$tmp/assets/package/agent-quota"

cat > "$tmp/mock/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo "$TEST_OS" ;;
  -m) echo "$TEST_ARCH" ;;
  *) exit 1 ;;
esac
EOF
cat > "$tmp/mock/curl" <<'EOF'
#!/bin/sh
set -eu
# Installer invokes curl -fsSL URL -o DEST.
name=${2##*/}
printf '%s\n' "$name" >> "$TEST_REQUESTS"
cp "$TEST_ASSETS/$name" "$4"
EOF
chmod +x "$tmp/mock/uname" "$tmp/mock/curl"
export TEST_ASSETS="$tmp/assets" TEST_REQUESTS="$tmp/requests"

for target in linux_amd64 darwin_amd64 darwin_arm64; do
  archive="agent-quota_1.2.3_${target}.tar.gz"
  tar -C "$tmp/assets" -czf "$tmp/assets/$archive" package
  (
    cd "$tmp/assets"
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$archive"
    else
      shasum -a 256 "$archive"
    fi
  ) >> "$tmp/assets/checksums.txt"
done

for platform in Linux:x86_64:linux_amd64 Darwin:x86_64:darwin_amd64 Darwin:arm64:darwin_arm64; do
  TEST_OS=${platform%%:*}
  rest=${platform#*:}
  TEST_ARCH=${rest%%:*}
  target=${rest#*:}
  export TEST_OS TEST_ARCH
  : > "$TEST_REQUESTS"
  PATH="$tmp/mock:$PATH" YES=1 VERSION=v1.2.3 BIN_DIR="$tmp/bin-$target" \
    sh "$root/scripts/install.sh" > "$tmp/output"
  grep -qx "agent-quota_1.2.3_${target}.tar.gz" "$TEST_REQUESTS"
  test -x "$tmp/bin-$target/agent-quota"
  test -L "$tmp/bin-$target/aq"
  test "$("$tmp/bin-$target/aq")" = fixture
  grep -q 'Run aq --help' "$tmp/output"
  printf 'installer %s: OK\n' "$target"
done

# The aq shortcut must also work with a relative custom installation directory.
(
  cd "$tmp"
  PATH="$tmp/mock:$PATH" YES=1 VERSION=v1.2.3 BIN_DIR=relative-bin \
    sh "$root/scripts/install.sh" > "$tmp/output"
  test -L relative-bin/aq
  test "$(relative-bin/aq)" = fixture
)
printf 'installer relative aq shortcut: OK\n'

# Unsupported targets must fail before downloading anything.
for platform in Linux:arm64 MINGW64_NT:x86_64 Darwin:i386; do
  export TEST_OS=${platform%:*} TEST_ARCH=${platform#*:}
  : > "$TEST_REQUESTS"
  if PATH="$tmp/mock:$PATH" YES=1 VERSION=v1.2.3 BIN_DIR="$tmp/unsupported" \
      sh "$root/scripts/install.sh" > "$tmp/output" 2>&1; then
    echo "installer accepted unsupported $platform" >&2
    exit 1
  fi
  test ! -s "$TEST_REQUESTS"
done

# A corrupt Mac archive must not replace an existing installation.
export TEST_OS=Darwin TEST_ARCH=arm64
printf corrupt >> "$tmp/assets/agent-quota_1.2.3_darwin_arm64.tar.gz"
if PATH="$tmp/mock:$PATH" YES=1 VERSION=v1.2.3 BIN_DIR="$tmp/bin-darwin_arm64" \
    sh "$root/scripts/install.sh" > "$tmp/output" 2>&1; then
  echo 'installer accepted invalid checksum' >&2
  exit 1
fi
test "$("$tmp/bin-darwin_arm64/aq")" = fixture
printf 'installer rejection/checksum tests: OK\n'
