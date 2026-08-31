package main

import (
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
	kindCount := map[string]int{}
	for p := range seen {
		if from[p] == to[p] {
			continue // unchanged
		}
		if pre, ok := firstPrefix(p, ignorePrefixes); ok {
			prefixCount[pre]++
			continue
		}
		if k := noiseKind(p); k != "" {
			kindCount[k]++
		}
	}

	out := []IgnoredBucket{}
	for _, pre := range ignorePrefixes {
		if c := prefixCount[pre]; c > 0 {
			out = append(out, IgnoredBucket{Label: pre, Count: c})
		}
	}
	for _, k := range []string{"*.pyc", "rpm-db", "apk-db", "dpkg-db"} {
		if c := kindCount[k]; c > 0 {
			out = append(out, IgnoredBucket{Label: k, Count: c})
		}
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
