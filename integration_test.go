//go:build integration

package main

import "testing"

// Exercises the real path — crane pull, tar flatten, go-rpmdb read, diff — against
// public images. Assertions are structural (not exact counts) so upstream tag
// refreshes don't make it flaky.
func TestIntegrationFedora(t *testing.T) {
	const plat = "linux/amd64"
	prefixes := []string{"etc/", "usr/"}

	from, err := readImage("quay.io/fedora/fedora:40", plat, prefixes, nil, true)
	if err != nil {
		t.Fatalf("read fedora:40: %v", err)
	}
	to, err := readImage("quay.io/fedora/fedora:41", plat, prefixes, nil, true)
	if err != nil {
		t.Fatalf("read fedora:41: %v", err)
	}

	if len(from.pkgs) < 50 || len(to.pkgs) < 50 {
		t.Fatalf("suspiciously few packages: %d -> %d", len(from.pkgs), len(to.pkgs))
	}
	if d := diff(from.pkgs, to.pkgs); len(d.Updated) == 0 {
		t.Fatal("expected updated packages between fedora 40 and 41")
	}
	// determinism / no false positives: an image diffed against itself is empty
	if d := diff(to.pkgs, to.pkgs); !d.empty() {
		t.Fatalf("self package-diff not empty: %+v", d)
	}
	if fd := fileDiff(to.files, to.files, prefixes); !fd.empty() {
		t.Fatalf("self file-diff not empty: %d groups", len(fd.Groups))
	}
}
