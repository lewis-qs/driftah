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

func diff(from, to map[string]Pkg) Diff {
	var d Diff
	for k, np := range to {
		op, ok := from[k]
		if !ok {
			d.Added = append(d.Added, np)
		} else if op.EVR() != np.EVR() {
			d.Updated = append(d.Updated, Update{Name: np.Name, Arch: np.Arch, From: op.EVR(), To: np.EVR()})
		}
	}
	for k, op := range from {
		if _, ok := to[k]; !ok {
			d.Removed = append(d.Removed, op)
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].key() < d.Added[j].key() })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].key() < d.Removed[j].key() })
	sort.Slice(d.Updated, func(i, j int) bool {
		return d.Updated[i].Name+d.Updated[i].Arch < d.Updated[j].Name+d.Updated[j].Arch
	})
	return d
}

func (d Diff) empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Updated) == 0
}
