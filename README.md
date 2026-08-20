# driftah

Diff two OCI images and print release notes — what packages and files drifted
between them.

`driftah` reads each image's rpm database and file tree directly out of its
layers — no `podman run`, no rpm-ostree, no ostree tooling. It reports:

- **Packages** added, updated, or removed (from the rpm database).
- **Files** added, modified, or removed under configurable path prefixes
  (git-style `A` / `M` / `R`), using content identity — so a rebuild that ships
  the same packages but different files is still caught.

Because it reads the rpm database and content hashes rather than the on-disk
layout, it works the same for conventional images and for ostree/bootc images
where content lives in the ostree object store.

## Usage

```
driftah [flags] <from-image> <to-image>

  --format markdown|json   output format (default markdown)
  --platform linux/amd64   platform to inspect for multi-arch images
  --paths etc/,usr/        comma-separated path prefixes to diff (empty to skip)
  --ignore ...             comma-separated path prefixes to omit from the file diff
  --ignore-packages ...    comma-separated package families to omit (e.g. kernel)
  --no-filter              keep noisy files (*.pyc, rpm db) in the file diff
  --title "..."            optional H1 title for markdown output
```

Both arguments are image references, resolved against the ambient container
registry credentials (`~/.docker/config.json`, `DOCKER_CONFIG`, or
`REGISTRY_AUTH_FILE`).

By default the file diff hides churn with no signal (`*.pyc`, the rpm database
files); pass `--no-filter` to keep them. Narrow scope with `--paths etc/,usr/lib`,
omit areas with `--ignore usr/lib64`, and drop noisy package families with
`--ignore-packages kernel`. The `--paths` prefixes also group the file output.

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
- uses: lewis-qs/driftah@v1
  id: notes
  with:
    from: ghcr.io/lewis-qs/bootc/almalinux:10.2-20260819
    to: ghcr.io/lewis-qs/bootc/almalinux:10.2-20260820
    args: --ignore-packages kernel
    output-file: notes.md
- run: gh release create "$VERSION" --notes-file notes.md
```

The generated markdown is also exposed as `${{ steps.notes.outputs.notes }}`.

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
