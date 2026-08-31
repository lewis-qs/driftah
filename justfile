set shell := ["bash", "-euc"]
set dotenv-load := false

root      := justfile_directory()
version   := env_var_or_default("VERSION", `git describe --tags --always --dirty 2>/dev/null || echo dev`)
ldflags   := "-s -w -X main.version=" + version
go_image   := env_var_or_default("GO_IMAGE", "docker.io/library/golang:1.26")
lint_image := env_var_or_default("LINT_IMAGE", "docker.io/golangci/golangci-lint:latest")
oci_image  := env_var_or_default("IMAGE", "ghcr.io/lewis-qs/driftah")

host_arch   := if arch() == "aarch64" { "arm64" } else if arch() == "x86_64" { "amd64" } else { arch() }
target_arch := env_var_or_default("ARCH", host_arch)

default: check build

# run a command in the pinned Go toolchain image
[private]
run image +args:
    podman run --rm -v "{{ root }}:/src:z" -w /src -e CGO_ENABLED=0 {{ image }} {{ args }}

# build the static binary in the pinned toolchain
[group('dev')]
build:
    podman run --rm -v "{{ root }}:/src:z" -w /src -e CGO_ENABLED=0 {{ go_image }} go build -trimpath -ldflags="{{ ldflags }}" -o driftah .

# run the unit tests
[group('dev')]
[group('ci')]
test:
    just run {{ go_image }} go test ./...

# format the source
[group('dev')]
fmt:
    just run {{ go_image }} gofmt -l -w .

# fail if any file needs formatting
[group('ci')]
fmt-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out="$(just run {{ go_image }} gofmt -l .)"
    [ -z "$out" ] || { echo "unformatted:"; echo "$out"; exit 1; }

# golangci-lint
[group('ci')]
lint:
    just run {{ lint_image }} golangci-lint run ./...

# govulncheck
[group('ci')]
vuln:
    just run {{ go_image }} go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# integration test: pull real images + a signed Fedora UKI
[group('ci')]
integration:
    #!/usr/bin/env bash
    set -euo pipefail
    podman run --rm -v "{{ root }}:/src:z" -w /src -e CGO_ENABLED=0 {{ go_image }} \
        bash -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq rpm2cpio cpio >/dev/null && go test -tags integration -run TestIntegration -v ./...'

# fmt-check + lint + vuln + test (lint's govet covers vet)
[group('ci')]
check: fmt-check lint vuln test

# lint commit messages in a range (CI passes the PR base/head shas)
[group('ci')]
commitlint from to:
    #!/usr/bin/env bash
    set -euo pipefail
    npm ci
    npx commitlint --from "{{ from }}" --to "{{ to }}" --verbose

# diff two images from source (e.g. just diff <from> <to> --format json)
[group('dev')]
diff from to *args:
    go run . {{ args }} "{{ from }}" "{{ to }}"

# cross-compile static release binaries into dist/ with the given version
[group('release')]
binaries version:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p dist
    for arch in amd64 arm64; do
        podman run --rm -v "{{ root }}:/src:z" -w /src \
            -e CGO_ENABLED=0 -e GOOS=linux -e "GOARCH=$arch" \
            {{ go_image }} go build -trimpath -ldflags="-s -w -X main.version={{ version }}" -o "dist/driftah-linux-$arch" .
    done
    ( cd dist && sha256sum driftah-linux-* > checksums.txt )

# build the container image for one arch
[group('deploy')]
oci tag="latest" arch=target_arch:
    podman build --platform "linux/{{ arch }}" --build-arg "VERSION={{ version }}" -t "{{ oci_image }}:{{ tag }}" -f Containerfile .

# log in to ghcr with GHCR_TOKEN / GITHUB_ACTOR from the environment
[private]
ghcr-login:
    echo "${GHCR_TOKEN}" | podman login ghcr.io -u "${GITHUB_ACTOR}" --password-stdin

# force-update the major (vN) moving tag to point at the given release
[private]
major-tag version:
    #!/usr/bin/env bash
    set -euo pipefail
    v="{{ version }}"
    git tag -f "v${v%%.*}" "v${v}"
    git push -f "https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" "v${v%%.*}"

# cut a semver release from conventional commits; emit published/version outputs
[group('release')]
semver:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -f .driftah-released
    npm ci
    npx semantic-release
    out="${GITHUB_OUTPUT:-/dev/stdout}"
    if [ -f .driftah-released ]; then
        { echo "published=true"; echo "version=$(cat .driftah-released)"; } >> "$out"
        echo "released $(cat .driftah-released)"
    else
        echo "published=false" >> "$out"
        echo "no release"
    fi
    rm -f .driftah-released

# build and push the image for one arch, tagged <version>-<arch>
[group('release')]
image-release version arch:
    just oci "{{ version }}-{{ arch }}" "{{ arch }}"
    podman push "{{ oci_image }}:{{ version }}-{{ arch }}"

# stitch the per-arch images into a multi-arch manifest and push <version> + latest
[group('release')]
manifest-release version:
    #!/usr/bin/env bash
    set -euo pipefail
    podman manifest create m
    podman manifest add m "docker://{{ oci_image }}:{{ version }}-amd64"
    podman manifest add m "docker://{{ oci_image }}:{{ version }}-arm64"
    for tag in "{{ version }}" latest; do
        podman manifest push --all m "docker://{{ oci_image }}:${tag}"
    done
    podman manifest rm m

# remove build artefacts
[group('dev')]
clean:
    rm -f "{{ root }}/driftah"
