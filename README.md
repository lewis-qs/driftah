# driftah

Diff two OCI images and print release notes — what packages and files drifted
between them.

`driftah` reads each image's rpm database and file tree directly out of its
layers — no `podman run`, no rpm-ostree, no ostree tooling. It reports:

- **Packages** added, updated, or removed (from the rpm database).
- **Files** added, modified, or removed under configurable path prefixes
  (git-style `A` / `M` / `R`) — so a rebuild that ships the same packages but
  different files is still caught.

It works for both conventional images and ostree/bootc images (where content
lives in the ostree object store). File identity is a per-file signal: a sha256
of the content for regular files, and the ostree object identity for ostree
hardlinks. The ostree identity also reflects file metadata (mode, ownership,
xattrs), so on ostree images a metadata-only change is reported as modified,
whereas on conventional images identity is content-only.

## Usage

```
driftah [flags] <from-image> <to-image>

  --format markdown|json   output format (default markdown)
  --platform linux/amd64   platform to inspect for multi-arch images
  --paths etc/,usr/        comma-separated path prefixes to diff (empty to skip)
  --ignore ...             comma-separated path prefixes to omit from the file diff
  --ignore-packages ...    comma-separated package families to omit (e.g. kernel)
  --highlight ...          comma-separated packages to list current versions for
  --short-versions         in Key versions, drop the epoch and dist tag (keep version-release)
  --no-filter              keep noisy files (*.pyc, rpm db) in the file diff
  --title "..."            optional H1 title for markdown output
```

`--highlight kernel,bootc,systemd,podman` adds a **Key versions** block (the
current version of each listed package, read from the `to` image) at the top of
the output — always shown, even when nothing changed. Absent packages are
omitted; installonly packages (e.g. kernel) list all installed versions. Add
`--short-versions` to drop the epoch and distribution tag while keeping the
version and release (`7:6.12.0-211.47.1.el10_2` → `6.12.0-211.47.1`).

Both arguments are image references, resolved against the ambient container
registry credentials (`~/.docker/config.json`, `DOCKER_CONFIG`, or
`REGISTRY_AUTH_FILE`).

By default the file diff hides churn with no signal (`*.pyc`, the rpm database
files); pass `--no-filter` to keep them. Narrow scope with `--paths etc/,usr/lib`,
omit areas with `--ignore usr/lib64`, and drop noisy package families with
`--ignore-packages kernel`. The `--paths` prefixes also group the file output.

Changes excluded by `--ignore` or the noise filter aren't listed, but each
category is counted on its own line under an **Ignored changes (not listed)**
heading (e.g. `- **2064** `usr/lib/.build-id/``) — objective visibility of the
volume without the noise.

### As a container

```
podman run --rm ghcr.io/lewis-qs/driftah \
  ghcr.io/lewis-qs/bootc/almalinux:10.2-20260819 \
  ghcr.io/lewis-qs/bootc/almalinux:10.2-20260820
```

For private registries, mount your auth file:

```
podman run --rm \
  -v $XDG_RUNTIME_DIR/containers/auth.json:/tmp/auth.json:ro \
  -e REGISTRY_AUTH_FILE=/tmp/auth.json \
  ghcr.io/lewis-qs/driftah <from> <to>
```

### As a GitHub Action

Consume it in a release workflow to generate the notes between two images:

```yaml
- uses: lewis-qs/driftah@v1        # pin @<sha>, and image:@sha256:… for a trusted supply chain
  id: notes
  with:
    from: ghcr.io/lewis-qs/bootc/almalinux:10.2-20260819
    to: ghcr.io/lewis-qs/bootc/almalinux:10.2-20260820
    args: --ignore-packages kernel
- env:
    NOTES: ${{ steps.notes.outputs.notes }}
  run: gh release create "$VERSION" --notes "$NOTES"
```

The markdown is exposed as the `notes` output; pass it through `env:` (as above)
rather than interpolating it into a `run:` line.

### Example output

```markdown
### Updated packages (2)

- **bash** `5.2.26-3.el10` → `5.2.32-1.el10`
- **curl** `8.9.1-4.el10` → `8.11.0-1.el10`

### Changed files in /etc (3)

- `M` etc/containers/policy.json
- `A` etc/containers/registries.d/almalinux-bootc.yaml
- `M` etc/pki/ca-trust/extracted/java/cacerts
```

## Build

```
just build   # static binary -> ./driftah
just test    # unit tests
just oci     # container image
```

## Roadmap

- Per-package `%changelog` entries under each updated package
- CVE / advisory enrichment from distribution errata
