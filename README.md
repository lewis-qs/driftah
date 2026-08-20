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
  --title "..."            optional H1 title for markdown output
```

Both arguments are image references, resolved against the ambient container
registry credentials (`~/.docker/config.json`, `DOCKER_CONFIG`, or
`REGISTRY_AUTH_FILE`).

Narrow the file diff to what you care about, e.g. `--paths etc/,usr/lib` or just
`--paths etc/`. The prefixes also decide how the file changes are grouped in the
output.

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
