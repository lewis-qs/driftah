package main

import (
	"sort"
	"strings"
)

type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added, modified, removed
	Owner  string `json:"owner,omitempty"`
}

type FileGroup struct {
	Prefix  string       `json:"prefix"`
	Changes []FileChange `json:"changes"`
}

type FileDiff struct {
	Groups []FileGroup `json:"groups"`
}

func fileDiff(from, to map[string]string, owners map[string]string, prefixes []string) FileDiff {
	byPrefix := map[string][]FileChange{}
	assign := func(c FileChange) {
		for _, pre := range prefixes {
			if strings.HasPrefix(c.Path, pre) {
				byPrefix[pre] = append(byPrefix[pre], c)
				return
			}
		}
	}
	own := func(p string) string {
		if owners == nil {
			return ""
		}
		return owners[p]
	}
	for p, tv := range to {
		if fv, ok := from[p]; !ok {
			assign(FileChange{Path: p, Status: "added", Owner: own(p)})
		} else if fv != tv {
			assign(FileChange{Path: p, Status: "modified", Owner: own(p)})
		}
	}
	for p := range from {
		if _, ok := to[p]; !ok {
			assign(FileChange{Path: p, Status: "removed", Owner: own(p)})
		}
	}

	fd := FileDiff{Groups: []FileGroup{}}
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

func mergeOwnerMaps(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
