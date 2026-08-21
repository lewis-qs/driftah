package main

import (
	"slices"
	"sort"
	"strings"
)

type KeyVersion struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

// keyVersions reports the current version(s) of each named package present in
// the image, in the order requested. Names not installed are omitted; a name
// with several installed versions (installonly, e.g. kernel) lists them all.
func keyVersions(pkgs map[string]Pkg, names []string, short bool) []KeyVersion {
	byName := map[string][]string{}
	for _, p := range pkgs {
		v := p.EVR()
		if short {
			v = shortEVR(p)
		}
		byName[p.Name] = append(byName[p.Name], v)
	}
	out := []KeyVersion{}
	for _, n := range names {
		if evrs, ok := byName[n]; ok {
			sort.Strings(evrs)
			out = append(out, KeyVersion{Name: n, Versions: slices.Compact(evrs)})
		}
	}
	return out
}

// shortEVR drops the epoch and the distribution/vendor tag (`.el10_2`,
// `.alma.1`, …) but keeps the version and package release, so the meaningful
// patch level survives (e.g. kernel `6.12.0-211.47.1.el10_2` -> `6.12.0-211.47.1`).
func shortEVR(p Pkg) string {
	rel := p.Release
	if i := strings.Index(rel, ".el"); i >= 0 {
		rel = rel[:i]
	}
	if rel == "" {
		return p.Version
	}
	return p.Version + "-" + rel
}
