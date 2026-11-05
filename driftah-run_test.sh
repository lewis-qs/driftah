#!/usr/bin/env bash
# Checks driftah-run.sh's classification and failure handling without a real
# container: a stub podman records its argv (and can be told to fail or emit
# chosen output) so we can assert what was mounted, which arguments were
# rewritten, and that failures propagate.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# stub podman: record argv (one per line), emit $PODMAN_OUT, exit $PODMAN_EXIT
cat > "$tmp/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$PODMAN_ARGV"
printf '%s' "${PODMAN_OUT-fake notes}"
[ -n "${PODMAN_OUT+x}" ] || echo
exit "${PODMAN_EXIT:-0}"
EOF
chmod +x "$tmp/podman"
export PATH="$tmp:$PATH"
export PODMAN_ARGV="$tmp/argv"
export DRIFTAH_IMAGE="img@sha256:dead"

# Run driftah-run.sh in a clean env for each case. Usage: run_case FROM TO [ARGS]
# Honours PODMAN_EXIT / PODMAN_OUT from the caller. Captures exit code in RC,
# script stdout+stderr in OUT, and the GITHUB_OUTPUT file contents in GHOUT.
run_case() {
  local out
  export GITHUB_OUTPUT="$tmp/ghout"; : > "$GITHUB_OUTPUT"
  : > "$PODMAN_ARGV"
  set +e
  OUT="$(FROM="$1" TO="$2" ARGS="${3-}" "$here/driftah-run.sh" 2>&1)"
  RC=$?
  set -e
  ARGV="$(cat "$PODMAN_ARGV")"
  GHOUT="$(cat "$GITHUB_OUTPUT")"
}
fail() { echo "FAIL: $1"; echo "rc=$RC"; echo "argv:"; echo "$ARGV"; echo "out:"; echo "$OUT"; exit 1; }

echo disk > "$tmp/disk.img"

# 1. local .img FROM + registry TO: FROM mounted & rewritten, TO passed through.
run_case "$tmp/disk.img" "ghcr.io/x/y:tag" "--short-versions"
abs="$(readlink -f -- "$tmp/disk.img")"
[ "$RC" = 0 ] || fail "case1 nonzero exit"
grep -qxF -- "$abs:/driftah/from/disk.img:ro" <<<"$ARGV" || fail "local file not bind-mounted read-only"
grep -qxF -- "/driftah/from/disk.img"          <<<"$ARGV" || fail "local FROM not rewritten"
grep -qxF -- "ghcr.io/x/y:tag"                 <<<"$ARGV" || fail "registry TO not passed through"
grep -qxF -- "--short-versions"                <<<"$ARGV" || fail "extra args dropped"
grep -q   -- "ghcr.io" <<<"$(grep -A1 -- '^-v$' <<<"$ARGV")" && fail "registry ref wrongly mounted"
grep -qF  -- "fake notes" <<<"$GHOUT"                     || fail "notes not captured"

# 2. symlink keeps its own name (so driftah still sees a .img), mounts the target.
ln -s disk.img "$tmp/link.img"
run_case "$tmp/link.img" "ghcr.io/x/y:tag"
[ "$RC" = 0 ] || fail "case2 nonzero exit"
grep -qxF -- "$abs:/driftah/from/link.img:ro" <<<"$ARGV" || fail "symlink: wrong mount source/name"
grep -qxF -- "/driftah/from/link.img"         <<<"$ARGV" || fail "symlink: arg lost its .img suffix"

# 3. two local files sharing a basename land in distinct slots.
mkdir -p "$tmp/a" "$tmp/b"; echo o > "$tmp/a/disk.img"; echo n > "$tmp/b/disk.img"
run_case "$tmp/a/disk.img" "$tmp/b/disk.img"
[ "$RC" = 0 ] || fail "case3 nonzero exit"
grep -qxF -- "$(readlink -f -- "$tmp/a/disk.img"):/driftah/from/disk.img:ro" <<<"$ARGV" || fail "from slot wrong"
grep -qxF -- "$(readlink -f -- "$tmp/b/disk.img"):/driftah/to/disk.img:ro"   <<<"$ARGV" || fail "to slot wrong"

# 4. both registry refs: nothing is mounted (auth is unset in this test env).
run_case "alpine:3.20" "alpine:3.21"
[ "$RC" = 0 ] || fail "case4 nonzero exit"
grep -qxF -- "-v" <<<"$ARGV" && fail "registry-only diff mounted something"

# 5. a local-looking path that does not exist is a hard error, not a silent pull.
run_case "$tmp/does-not-exist.img" "ghcr.io/x/y:tag"
[ "$RC" != 0 ] || fail "missing local .img did not error"
grep -qF -- "::error::" <<<"$OUT" || fail "missing local .img gave no ::error::"

# 6. a failing podman propagates (no swallowed container failure).
PODMAN_EXIT=7 run_case "ghcr.io/x/y:a" "ghcr.io/x/y:b"
[ "$RC" != 0 ] || fail "podman failure was swallowed"

# 7. empty driftah output warns but still succeeds (valid "no differences").
PODMAN_OUT="" run_case "ghcr.io/x/y:a" "ghcr.io/x/y:b"
[ "$RC" = 0 ] || fail "empty output should still succeed"
grep -qF -- "::warning::" <<<"$OUT" || fail "empty output gave no ::warning::"

echo "PASS"
