package main

import (
	"path"
	"strings"
)

type IgnoredBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// ignoredChanges counts how many ignored/noise files changed between the two
// images, grouped by why they were excluded, so the notes can flag an anomaly
// (e.g. thousands of usr/lib/.build-id churn where there are normally a handful)
// with a one-line summary instead of listing every path.
func ignoredChanges(from, to map[string]string, ignorePrefixes []string, filterNoise bool) []IgnoredBucket {
	seen := map[string]bool{}
	for p := range from {
		seen[p] = true
	}
	for p := range to {
		seen[p] = true
	}

	prefixCount := map[string]int{}
	pyc, rpmdb := 0, 0
	for p := range seen {
		if from[p] == to[p] {
			continue // unchanged
		}
		if pre, ok := firstPrefix(p, ignorePrefixes); ok {
			prefixCount[pre]++
			continue
		}
		// not prefix-matched, so it is in the ignored set only because it is noise
		if strings.HasSuffix(path.Base(p), ".pyc") {
			pyc++
		} else {
			rpmdb++
		}
	}

	out := []IgnoredBucket{}
	for _, pre := range ignorePrefixes { // keep the operator's order
		if c := prefixCount[pre]; c > 0 {
			out = append(out, IgnoredBucket{Label: pre, Count: c})
		}
	}
	if pyc > 0 {
		out = append(out, IgnoredBucket{Label: "*.pyc", Count: pyc})
	}
	if rpmdb > 0 {
		out = append(out, IgnoredBucket{Label: "rpm-db", Count: rpmdb})
	}
	return out
}

func firstPrefix(s string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return p, true
		}
	}
	return "", false
}
