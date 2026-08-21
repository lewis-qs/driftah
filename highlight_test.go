package main

import "testing"

func TestKeyVersions(t *testing.T) {
	to := index(
		mk("bash", "5.2"),
		mk("kernel", "6.12"),
		Pkg{Name: "kernel", Version: "6.13", Release: "1", Arch: "x86_64"},
		// same EVR, two arches -> must collapse to one version
		Pkg{Name: "glibc", Version: "2.34", Release: "1", Arch: "x86_64"},
		Pkg{Name: "glibc", Version: "2.34", Release: "1", Arch: "aarch64"},
	)
	kvs := keyVersions(to, []string{"kernel", "bootc", "bash", "glibc"}, false)

	// bootc is absent -> omitted; requested order is preserved (kernel, bash, glibc)
	if len(kvs) != 3 {
		t.Fatalf("got %d key versions: %+v", len(kvs), kvs)
	}
	if kvs[0].Name != "kernel" || len(kvs[0].Versions) != 2 {
		t.Errorf("kernel = %+v (want 2 versions)", kvs[0])
	}
	if kvs[1].Name != "bash" || len(kvs[1].Versions) != 1 {
		t.Errorf("bash = %+v (want 1 version)", kvs[1])
	}
	if kvs[2].Name != "glibc" || len(kvs[2].Versions) != 1 {
		t.Errorf("glibc = %+v (want 1 version after arch dedup)", kvs[2])
	}

	// short mode keeps version-release but strips the dist/vendor tag + epoch
	dist := index(
		Pkg{Name: "kernel", Version: "6.12.0", Release: "211.47.1.el10_2", Arch: "x86_64"},
		Pkg{Name: "bootc", Version: "1.15.2", Release: "1.el10_2.alma.1", Arch: "x86_64"},
		Pkg{Name: "podman", Epoch: "7", Version: "5.8.2", Release: "5.el10_2.alma.1", Arch: "x86_64"},
	)
	want := map[string]string{"kernel": "6.12.0-211.47.1", "bootc": "1.15.2-1", "podman": "5.8.2-5"}
	for _, kv := range keyVersions(dist, []string{"kernel", "bootc", "podman"}, true) {
		if kv.Versions[0] != want[kv.Name] {
			t.Errorf("short %s = %q (want %q)", kv.Name, kv.Versions[0], want[kv.Name])
		}
	}
}
