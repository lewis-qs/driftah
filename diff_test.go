package main

import "testing"

func mk(name, ver string) Pkg { return Pkg{Name: name, Version: ver, Release: "1", Arch: "x86_64"} }

func index(ps ...Pkg) map[string]Pkg {
	m := map[string]Pkg{}
	for _, p := range ps {
		m[p.nevraKey()] = p
	}
	return m
}

func TestDiff(t *testing.T) {
	from := index(mk("bash", "5.2"), mk("curl", "8.6"), mk("oldpkg", "1.0"))
	to := index(mk("bash", "5.2"), mk("curl", "8.9"), mk("newpkg", "2.0"))

	d := diff(from, to)
	if len(d.Added) != 1 || d.Added[0].Name != "newpkg" {
		t.Fatalf("added = %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].Name != "oldpkg" {
		t.Fatalf("removed = %+v", d.Removed)
	}
	if len(d.Updated) != 1 || d.Updated[0].Name != "curl" {
		t.Fatalf("updated = %+v", d.Updated)
	}
	if d.Updated[0].From != "8.6-1" || d.Updated[0].To != "8.9-1" {
		t.Fatalf("update evr = %+v", d.Updated[0])
	}
}

func TestDiffInstallonly(t *testing.T) {
	kern := func(v string) Pkg { return Pkg{Name: "kernel", Version: v, Release: "1", Arch: "x86_64"} }
	from := index(kern("5.14.0-A"), kern("5.14.0-B"))
	to := index(kern("5.14.0-B"), kern("5.14.0-C"))
	d := diff(from, to)
	if len(d.Updated) != 0 {
		t.Fatalf("installonly kernel must not be reported as updated: %+v", d.Updated)
	}
	if len(d.Added) != 1 || d.Added[0].Version != "5.14.0-C" {
		t.Fatalf("added = %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].Version != "5.14.0-A" {
		t.Fatalf("removed = %+v", d.Removed)
	}
}

func TestFileDiff(t *testing.T) {
	from := map[string]string{"etc/a.conf": "H:1", "etc/gone": "H:9", "usr/bin/x": "L:objA"}
	to := map[string]string{"etc/a.conf": "H:2", "etc/new": "H:3", "usr/bin/x": "L:objA"}
	fd := fileDiff(from, to, []string{"etc/", "usr/"})
	byPrefix := map[string][]FileChange{}
	for _, g := range fd.Groups {
		byPrefix[g.Prefix] = g.Changes
	}
	if len(byPrefix["usr/"]) != 0 {
		t.Fatalf("usr should be unchanged (identical identity): %+v", byPrefix["usr/"])
	}
	got := map[string]string{}
	for _, c := range byPrefix["etc/"] {
		got[c.Path] = c.Status
	}
	want := map[string]string{"etc/a.conf": "modified", "etc/new": "added", "etc/gone": "removed"}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("%s: got %q want %q", p, got[p], s)
		}
	}
}

func TestEVR(t *testing.T) {
	cases := []struct {
		p    Pkg
		want string
	}{
		{Pkg{Version: "1.2", Release: "3"}, "1.2-3"},
		{Pkg{Epoch: "0", Version: "1.2", Release: "3"}, "1.2-3"},
		{Pkg{Epoch: "1", Version: "1.2", Release: "3"}, "1:1.2-3"},
	}
	for _, c := range cases {
		if got := c.p.EVR(); got != c.want {
			t.Errorf("EVR(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}
