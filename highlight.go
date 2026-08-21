package main

import "sort"

type KeyVersion struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

// keyVersions reports the current version(s) of each named package present in
// the image, in the order requested. Names not installed are omitted; a name
// with several installed versions (installonly, e.g. kernel) lists them all.
func keyVersions(pkgs map[string]Pkg, names []string) []KeyVersion {
	byName := map[string][]string{}
	for _, p := range pkgs {
		byName[p.Name] = append(byName[p.Name], p.EVR())
	}
	out := []KeyVersion{}
	for _, n := range names {
		if evrs, ok := byName[n]; ok {
			sort.Strings(evrs)
			out = append(out, KeyVersion{Name: n, Versions: evrs})
		}
	}
	return out
}
