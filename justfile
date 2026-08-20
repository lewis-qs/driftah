set shell := ["bash", "-euc"]

oci_image := env_var_or_default("IMAGE", "ghcr.io/lewis-qs/driftah")

# build the static binary
[group('dev')]
build:
    CGO_ENABLED=0 go build -ldflags="-s -w" -o driftah .

# run the unit tests
[group('dev')]
test:
    go test ./...

# diff two images from source (e.g. just run <from> <to> --format json)
[group('dev')]
run from to *args:
    go run . {{args}} "{{from}}" "{{to}}"

# build the container image
[group('release')]
oci tag="latest":
    podman build -t "{{oci_image}}:{{tag}}" .
