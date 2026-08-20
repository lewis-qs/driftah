package main

import "sort"

type Update struct {
	Name string `json:"name"`
	Arch string `json:"arch"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Diff struct {
	Added   []Pkg    `json:"added"`
	Removed []Pkg    `json:"removed"`
	Updated []Update `json:"updated"`
}

// diff compares two package inventories keyed by full NEVRA. Packages are
// grouped by name+arch: a name+arch with exactly one instance on each side is a
// clean update (or unchanged); anything with multiple installed instances
// (installonly packages like the kernel family) is reported as exact per-version
// adds/removes rather than being forced into a misleading single "update".
func diff(from, to map[string]Pkg) Diff {
	fromNA := groupByNameArch(from)
	toNA := groupByNameArch(to)

	keys := map[string]bool{}
	for k := range fromNA {
		keys[k] = true
	}
	for k := range toNA {
		keys[k] = true
	}

	var d Diff
	for k := range keys {
		fs, ts := fromNA[k], toNA[k]
		if len(fs) == 1 && len(ts) == 1 {
			if fs[0].EVR() != ts[0].EVR() {
				d.Updated = append(d.Updated, Update{Name: ts[0].Name, Arch: ts[0].Arch, From: fs[0].EVR(), To: ts[0].EVR()})
			}
			continue
		}
		fevr, tevr := evrSet(fs), evrSet(ts)
		for _, p := range ts {
			if !fevr[p.EVR()] {
				d.Added = append(d.Added, p)
			}
		}
		for _, p := range fs {
			if !tevr[p.EVR()] {
				d.Removed = append(d.Removed, p)
			}
		}
	}

	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].nevraKey() < d.Added[j].nevraKey() })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].nevraKey() < d.Removed[j].nevraKey() })
	sort.Slice(d.Updated, func(i, j int) bool {
		return d.Updated[i].Name+"."+d.Updated[i].Arch < d.Updated[j].Name+"."+d.Updated[j].Arch
	})
	return d
}

func groupByNameArch(m map[string]Pkg) map[string][]Pkg {
	r := map[string][]Pkg{}
	for _, p := range m {
		r[p.key()] = append(r[p.key()], p)
	}
	return r
}

func evrSet(ps []Pkg) map[string]bool {
	s := map[string]bool{}
	for _, p := range ps {
		s[p.EVR()] = true
	}
	return s
}

func (d Diff) empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Updated) == 0
}
