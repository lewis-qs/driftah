#!/usr/bin/env bash
# Entrypoint for the composite action: diff FROM against TO with driftah in its
# container and capture the notes to GITHUB_OUTPUT.
#
# A FROM/TO that names a local image file (a .img disk or .efi/.uki) is
# bind-mounted read-only into the container and its argument rewritten to the
# in-container path, because the container cannot otherwise see the runner's
# filesystem. Anything else is passed through unchanged as a registry reference
# for driftah to pull.
#
# -f disables pathname expansion: nothing here globs on purpose, and it stops a
# '*' or '?' in $ARGS (word-split into flags below) from expanding against the
# working directory.
set -euf -o pipefail

: "${DRIFTAH_IMAGE:?}" "${FROM:?}" "${TO:?}"
ARGS="${ARGS:-}"

mounts=()

# Registry auth, if the runner has it (same lookup driftah's own pull needs).
auth=""
if [ -n "${REGISTRY_AUTH_FILE:-}" ]; then
  if [ -f "${REGISTRY_AUTH_FILE}" ]; then
    auth="${REGISTRY_AUTH_FILE}"
  else
    # Set but not a file: a misconfiguration worth surfacing rather than
    # silently falling back and later failing with an opaque "unauthorized".
    echo "::warning::REGISTRY_AUTH_FILE is set to '${REGISTRY_AUTH_FILE}' but that is not a file; ignoring it"
  fi
fi
if [ -z "$auth" ] && [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -f "${XDG_RUNTIME_DIR}/containers/auth.json" ]; then
  auth="${XDG_RUNTIME_DIR}/containers/auth.json"
fi
[ -n "$auth" ] && mounts+=(-v "$auth:/tmp/auth.json:ro" -e REGISTRY_AUTH_FILE=/tmp/auth.json)

# Classify a ref and set REPLY to the argument driftah should receive, appending
# a mount when it is a local file. The rule mirrors driftah's own likelyUKI
# (main.go): a *regular* file whose name ends in .efi/.img/.uki is local;
# everything else - a registry ref, or a directory that merely shares that name
# - is passed through, so the action never bind-mounts a host path driftah would
# not itself have read as a file. A ref that looks local by suffix but is not a
# regular file is a hard error, not a silent reclassification to a pull, because
# that is almost always a wrong/missing path and a silent pull failure is
# undebuggable. The mount source is the resolved path (readlink -f, so a symlink
# works), but the in-container name keeps the ORIGINAL basename so the suffix
# driftah keys on survives a symlink. Slots are separate so two files sharing a
# basename (disk.img vs disk.img) do not collide on one mount target.
# $1 = ref, $2 = slot (from|to). Called without command substitution so the
# mounts append lands in this shell, not a subshell.
localise() {
  local ref="$1" slot="$2" abs base
  case "${ref,,}" in
    *.efi | *.img | *.uki)
      if [ ! -f "$ref" ]; then
        echo "::error::$slot '$ref' looks like a local image (.efi/.img/.uki) but is not a readable regular file"
        exit 1
      fi
      abs="$(readlink -f -- "$ref")"
      base="$(basename -- "$ref")"
      mounts+=(-v "$abs:/driftah/$slot/$base:ro")
      REPLY="/driftah/$slot/$base"
      echo "driftah: $slot is local file $ref (mounted at /driftah/$slot/$base)"
      ;;
    *)
      REPLY="$ref"
      echo "driftah: $slot is registry ref $ref"
      ;;
  esac
}

localise "$FROM" from; from_arg="$REPLY"
localise "$TO" to;     to_arg="$REPLY"

# $ARGS is deliberately unquoted so multiple flags word-split into arguments.
notes="$(podman run --rm "${mounts[@]}" "$DRIFTAH_IMAGE" $ARGS "$from_arg" "$to_arg")"

# Empty output is a valid "no differences" result, but it is also what a silent
# problem would leave behind, so flag it rather than publishing blank notes
# without a trace.
if [ -z "${notes//[$' \t\n']/}" ]; then
  echo "::warning::driftah produced no output (either no differences, or a problem that still exited 0)"
fi

delim="driftah_$(head -c16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
case "$notes" in *"$delim"*) echo "::error::notes delimiter collision"; exit 1 ;; esac
# GITHUB_OUTPUT is required: no /dev/stdout fallback, so that running outside a
# step (where the capture would be lost) fails loudly under set -u instead of
# silently discarding the notes.
{
  echo "notes<<$delim"
  printf '%s\n' "$notes"
  echo "$delim"
} >> "$GITHUB_OUTPUT"
