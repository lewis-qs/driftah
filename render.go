package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func renderMarkdown(rep Report, title, fromRef, toRef string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	fmt.Fprintf(&b, "**From:** `%s`  \n**To:** `%s`\n\n", fromRef, toRef)

	d := rep.Packages
	if d.empty() && rep.Files.empty() {
		b.WriteString("No changes.\n")
		return b.String()
	}

	if len(d.Updated) > 0 {
		fmt.Fprintf(&b, "### Updated packages (%d)\n\n", len(d.Updated))
		for _, u := range d.Updated {
			fmt.Fprintf(&b, "- **%s** `%s` → `%s`\n", u.Name, u.From, u.To)
		}
		b.WriteString("\n")
	}
	pkgSection(&b, "Added packages", d.Added)
	pkgSection(&b, "Removed packages", d.Removed)

	for _, g := range rep.Files.Groups {
		fileSection(&b, "/"+strings.TrimSuffix(g.Prefix, "/"), g.Changes)
	}
	return b.String()
}

func pkgSection(b *strings.Builder, title string, pkgs []Pkg) {
	if len(pkgs) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s (%d)\n\n", title, len(pkgs))
	for _, p := range pkgs {
		fmt.Fprintf(b, "- %s `%s`\n", p.Name, p.EVR())
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
		fmt.Fprintf(b, "- `%s` %s\n", statusMark[c.Status], c.Path)
	}
	b.WriteString("\n")
}

func renderJSON(rep Report) (string, error) {
	out, err := json.MarshalIndent(rep, "", "  ")
	return string(out), err
}
