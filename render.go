package main

import (
	"fmt"
	"strings"
)

// mdCode wraps a value in an inline code span and neutralises any backtick, so
// image-derived strings (package names, file paths) can't inject markdown.
func mdCode(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "'") + "`"
}

func renderMarkdown(rep Report, title, fromRef, toRef string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	fmt.Fprintf(&b, "**From:** %s  \n**To:** %s\n\n", mdCode(fromRef), mdCode(toRef))
	keyVersionsSection(&b, rep.KeyVersions)

	d := rep.Packages
	if d.empty() && rep.Files.empty() {
		b.WriteString("No changes.\n")
		ignoredSection(&b, rep.Ignored)
		return b.String()
	}

	if len(d.Updated) > 0 {
		fmt.Fprintf(&b, "### Updated packages (%d)\n\n", len(d.Updated))
		for _, u := range d.Updated {
			fmt.Fprintf(&b, "- **%s** %s → %s\n", mdCode(u.Name), mdCode(u.From), mdCode(u.To))
		}
		b.WriteString("\n")
	}
	pkgSection(&b, "Added packages", d.Added)
	pkgSection(&b, "Removed packages", d.Removed)

	for _, g := range rep.Files.Groups {
		fileSection(&b, "/"+strings.TrimSuffix(g.Prefix, "/"), g.Changes)
	}
	ignoredSection(&b, rep.Ignored)
	return b.String()
}

// ignoredSection prints a one-line count of the file changes that were excluded
// from the diff (by --ignore or the noise filter) — objective visibility, no
// listing: e.g. `Ignored: 2064 changes in `usr/lib/.build-id/`, 12 `*.pyc“.
func ignoredSection(b *strings.Builder, buckets []IgnoredBucket) {
	if len(buckets) == 0 {
		return
	}
	parts := make([]string, len(buckets))
	for i, bk := range buckets {
		parts[i] = fmt.Sprintf("%d %s", bk.Count, mdCode(bk.Label))
	}
	fmt.Fprintf(b, "\n_Ignored changes (not listed): %s._\n", strings.Join(parts, ", "))
}

func keyVersionsSection(b *strings.Builder, kvs []KeyVersion) {
	if len(kvs) == 0 {
		return
	}
	b.WriteString("### Key versions\n\n")
	for _, kv := range kvs {
		// kv.Name is operator-supplied (--highlight), so it's trusted and left
		// raw; only kv.Versions come from the image, so only those get mdCode.
		vs := make([]string, len(kv.Versions))
		for i, v := range kv.Versions {
			vs[i] = mdCode(v)
		}
		fmt.Fprintf(b, "- **%s** %s\n", kv.Name, strings.Join(vs, ", "))
	}
	b.WriteString("\n")
}

func pkgSection(b *strings.Builder, title string, pkgs []Pkg) {
	if len(pkgs) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s (%d)\n\n", title, len(pkgs))
	for _, p := range pkgs {
		fmt.Fprintf(b, "- %s %s\n", mdCode(p.Name), mdCode(p.EVR()))
	}
	b.WriteString("\n")
}

var statusMark = map[string]string{"added": "A", "modified": "M", "removed": "R"}

func fileSection(b *strings.Builder, title string, changes []FileChange) {
	if len(changes) == 0 {
		return
	}
	fmt.Fprintf(b, "### Changed files in %s (%d)\n\n", title, len(changes))
	for _, c := range changes {
		fmt.Fprintf(b, "- `%s` %s\n", statusMark[c.Status], mdCode(c.Path))
	}
	b.WriteString("\n")
}
