package main

import "testing"

func TestKeyVersions(t *testing.T) {
	to := index(
		mk("bash", "5.2"),
		mk("kernel", "6.12"),
		Pkg{Name: "kernel", Version: "6.13", Release: "1", Arch: "x86_64"},
	)
	kvs := keyVersions(to, []string{"kernel", "bootc", "bash"})

	// bootc is absent -> omitted; requested order is preserved (kernel, bash)
	if len(kvs) != 2 {
		t.Fatalf("got %d key versions: %+v", len(kvs), kvs)
	}
	if kvs[0].Name != "kernel" || len(kvs[0].Versions) != 2 {
		t.Errorf("kernel = %+v (want 2 versions)", kvs[0])
	}
	if kvs[1].Name != "bash" || len(kvs[1].Versions) != 1 {
		t.Errorf("bash = %+v (want 1 version)", kvs[1])
	}
}
