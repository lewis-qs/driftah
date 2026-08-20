package main

import (
	"sort"
	"strings"
)

type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added, modified, removed
}

type FileGroup struct {
	Prefix  string       `json:"prefix"`
	Changes []FileChange `json:"changes"`
}

type FileDiff struct {
	Groups []FileGroup `json:"groups"`
}

// fileDiff compares two content-identity maps and buckets each changed path
// under the first matching prefix, preserving the order the prefixes were given.
func fileDiff(from, to map[string]string, prefixes []string) FileDiff {
	byPrefix := map[string][]FileChange{}
	assign := func(c FileChange) {
		for _, pre := range prefixes {
			if strings.HasPrefix(c.Path, pre) {
				byPrefix[pre] = append(byPrefix[pre], c)
				return
			}
		}
	}
	for p, tv := range to {
		if fv, ok := from[p]; !ok {
			assign(FileChange{p, "added"})
		} else if fv != tv {
			assign(FileChange{p, "modified"})
		}
	}
	for p := range from {
		if _, ok := to[p]; !ok {
			assign(FileChange{p, "removed"})
		}
	}

	var fd FileDiff
	for _, pre := range prefixes {
		cs := byPrefix[pre]
		if len(cs) == 0 {
			continue
		}
		sort.Slice(cs, func(i, j int) bool { return cs[i].Path < cs[j].Path })
		fd.Groups = append(fd.Groups, FileGroup{Prefix: pre, Changes: cs})
	}
	return fd
}

func (f FileDiff) empty() bool { return len(f.Groups) == 0 }
