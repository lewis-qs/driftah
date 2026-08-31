# driftah

Diff two OCI images, systemd UKIs, or disk images and print release notes —
what packages and files drifted between them.

`driftah` reads each image's package database and file tree directly out of its
layers — no `podman run`, no rpm-ostree, no ostree tooling. Local `.efi` UKIs
([systemd UKI](https://uapi-group.org/specifications/specs/unified_kernel_image/),
as built by `ukify`) and `.img` disk images with a UKI on the ESP are read the
same way: PE sections, then the embedded initramfs.

It reports:

- **Packages** added, updated, or removed (rpm, apk, or deb).
- **Files** added, modified, or removed under configurable path prefixes
  (git-style `A` / `M` / `R`).
- **UKI** cmdline, os-release, and section hashes (`.linux`, `.initrd`, `.osrel`,
  `.cmdline`, `.dtb`, `.sbat`, …) when the inputs are Unified Kernel Images.

## Package databases

| Format | Where it looks |
|--------|----------------|
| rpm | `rpmdb.sqlite` (also via ostree hardlinks on bootc images) |
| apk | `lib/apk/db/installed` |
| deb | `var/lib/dpkg/status` |

Those paths are checked in OCI containers, bootc/ostree images, and UKI
initramfs alike. If the database was stripped, there is no package diff —
driftah still compares the file tree (and UKI sections). Widen `--paths` if
the interesting files sit outside `etc/` and `usr/` (e.g. `--paths etc/,usr/,bin/,lib/`).

On ostree/bootc images, file identity is the ostree object (content plus
metadata). On conventional images it is a sha256 of the content.

## Usage

```
driftah [flags] <from> <to>

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

Arguments are OCI image references, a UKI (`.efi`), or a GPT/MBR disk image
(`.img`) with a UKI on the ESP. Image references use the ambient container
registry credentials (`~/.docker/config.json`, `DOCKER_CONFIG`, or
`REGISTRY_AUTH_FILE`).

```
driftah ghcr.io/lewis-qs/bootc/almalinux:10.2-20260819 \
        ghcr.io/lewis-qs/bootc/almalinux:10.2-20260820
driftah docker.io/library/alpine:3.21 docker.io/library/alpine:edge
driftah docker.io/library/debian:stable docker.io/library/debian:unstable
driftah ukify-old.efi ukify-new.efi
driftah disk-old.img disk-new.img
```

By default the file diff hides churn with no signal (`*.pyc`, the rpm database
files); pass `--no-filter` to keep them. Narrow scope with `--paths etc/,usr/lib`,
omit areas with `--ignore usr/lib64`, and drop noisy package families with
`--ignore-packages kernel`. The `--paths` prefixes also group the file output.

Changes excluded by `--ignore` or the noise filter aren't listed, but each
category is counted on its own line under an **Ignored changes (not listed)**
heading (e.g. `- **2064** `usr/lib/.build-id/``).

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

The markdown is the `notes` output; pass it through `env:` rather than
interpolating it into a `run:` line. `from` / `to` can also be a UKI or disk
image if those files are available to the action's `podman run`.

### Example output

```markdown
### UKI

- **arch** `x86_64`
- **signed** `true`
- **cmdline** `console=ttyS0 quiet`
- **os-release** `NAME=Alpine Linux; ID=alpine; VERSION_ID=3.22`

### UKI sections (2)

- `M` `.osrel` `VERSION_ID=3.21` → `VERSION_ID=3.22`
- `M` `.linux` `sha256:abc123def456` → `sha256:fed654cba321`

### Updated packages (2)

- **busybox** `1.36.1-r31` → `1.37.0-r14`
- **musl** `1.2.5-r3` → `1.2.5-r11`

### Changed files in /etc (1)

- `M` `etc/os-release`
```

## Build

```
just build         # static binary -> ./driftah
just test          # unit tests
just integration   # pull live Fedora/Alpine/Debian images and a signed Fedora UKI
just oci           # container image
```

## Roadmap

- Per-package `%changelog` entries under each updated package
- CVE / advisory enrichment from distribution errata
