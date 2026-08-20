# rpmdrift

Diff the rpm package sets of two OCI images and print release notes.

`rpmdrift` reads each image's rpm database directly out of its layers — no
`podman run`, no rpm-ostree, no ostree tooling — and reports which packages were
added, updated, or removed between them. Because it reads the rpm database
rather than the on-disk layout, it works the same for conventional images and
for ostree/bootc images where the database lives in the ostree object store.

## Usage

```
rpmdrift [flags] <from-image> <to-image>

  --format markdown|json   output format (default markdown)
  --platform linux/amd64   platform to inspect for multi-arch images
  --title "..."            optional H1 title for markdown output
```

Both arguments are image references, resolved against the ambient container
registry credentials (`~/.docker/config.json`, `DOCKER_CONFIG`, or a mounted
auth file).

### As a container

```
podman run --rm ghcr.io/lewis-qs/rpmdrift \
  ghcr.io/lewis-qs/bootc/almalinux:10-20260101 \
  ghcr.io/lewis-qs/bootc/almalinux:10-20260108
```

For private registries, mount your auth file:

```
podman run --rm \
  -v $XDG_RUNTIME_DIR/containers/auth.json:/tmp/auth.json:ro \
  -e REGISTRY_AUTH_FILE=/tmp/auth.json \
  ghcr.io/lewis-qs/rpmdrift <from> <to>
```

### Example output

```markdown
### Updated packages (410)

- **NetworkManager** `1:1.54.3-4.el9_8` → `1:1.56.0-2.el10_2`
- **bash** `5.2.26-3.el9` → `5.2.32-1.el10`
...

### Added packages (48)
...

### Removed packages (30)
...
```

## Build

```
just build   # static binary -> ./rpmdrift
just test    # unit tests
just oci     # container image
```

## Roadmap

- Per-package `%changelog` entries under each updated package
- CVE / advisory enrichment from distribution errata
