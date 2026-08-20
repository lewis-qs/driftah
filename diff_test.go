package main

import "testing"

func mk(name, ver string) Pkg { return Pkg{Name: name, Version: ver, Release: "1", Arch: "x86_64"} }

func TestDiff(t *testing.T) {
	index := func(ps ...Pkg) map[string]Pkg {
		m := map[string]Pkg{}
		for _, p := range ps {
			m[p.key()] = p
		}
		return m
	}
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
