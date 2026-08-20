set shell := ["bash", "-euc"]
set dotenv-load := false

root      := justfile_directory()
version   := env_var_or_default("VERSION", `git describe --tags --always --dirty 2>/dev/null || echo dev`)
ldflags   := "-s -w -X main.version=" + version
go_image  := env_var_or_default("GO_IMAGE", "docker.io/library/golang:1.26")
oci_image := env_var_or_default("IMAGE", "ghcr.io/lewis-qs/driftah")

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
    just run {{ go_image }} go build -trimpath -ldflags="{{ ldflags }}" -o driftah .

# build with the host Go toolchain
[group('dev')]
build-local:
    CGO_ENABLED=0 go build -trimpath -ldflags="{{ ldflags }}" -o driftah .

# run the unit tests
[group('dev')]
[group('ci')]
test:
    just run {{ go_image }} go test ./...

# go vet
[group('ci')]
vet:
    just run {{ go_image }} go vet ./...

# format the source
[group('dev')]
fmt:
    just run {{ go_image }} gofmt -l -w .

# vet + test
[group('ci')]
check: vet test

# diff two images from source (e.g. just diff <from> <to> --format json)
[group('dev')]
diff from to *args:
    go run . {{ args }} "{{ from }}" "{{ to }}"

# build the container image for one arch
[group('deploy')]
oci tag="latest" arch=target_arch:
    podman build --platform "linux/{{ arch }}" --build-arg "VERSION={{ version }}" -t "{{ oci_image }}:{{ tag }}" -f Containerfile .

# remove build artefacts
[group('dev')]
clean:
    rm -f "{{ root }}/driftah"
