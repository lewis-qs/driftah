package main

import (
	"fmt"
	"strconv"
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
	ukiIdentity(&b, rep.UKI)

	d := rep.Packages
	if d.empty() && rep.Files.empty() && rep.UKI.empty() {
		b.WriteString("No changes.\n")
		ignoredSection(&b, rep.Ignored)
		return b.String()
	}
	ukiChanges(&b, rep.UKI)

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

// ignoredSection prints one line per category of file changes excluded from the
// diff (by --ignore or the noise filter) — objective counts, not a listing.
func ignoredSection(b *strings.Builder, buckets []IgnoredBucket) {
	if len(buckets) == 0 {
		return
	}
	b.WriteString("### Ignored changes (not listed)\n\n")
	for _, bk := range buckets {
		fmt.Fprintf(b, "- **%d** %s\n", bk.Count, mdCode(bk.Label))
	}
	b.WriteString("\n")
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

func ukiIdentity(b *strings.Builder, u *UKIReport) {
	if u == nil {
		return
	}
	b.WriteString("### UKI\n\n")
	fmt.Fprintf(b, "- **arch** %s\n", mdCode(u.Arch))
	fmt.Fprintf(b, "- **signed** %s\n", mdCode(strconv.FormatBool(u.Signed)))
	if u.Cmdline != "" {
		fmt.Fprintf(b, "- **cmdline** %s\n", mdCode(u.Cmdline))
	}
	if u.OSRel != "" {
		fmt.Fprintf(b, "- **os-release** %s\n", mdCode(strings.ReplaceAll(u.OSRel, "\n", "; ")))
	}
	b.WriteString("\n")
}

func ukiChanges(b *strings.Builder, u *UKIReport) {
	if u == nil || len(u.Changes) == 0 {
		return
	}
	fmt.Fprintf(b, "### UKI sections (%d)\n\n", len(u.Changes))
	for _, c := range u.Changes {
		switch {
		case c.From == "":
			fmt.Fprintf(b, "- `A` %s %s\n", mdCode(c.Name), mdCode(shortDisp(c.To)))
		case c.To == "":
			fmt.Fprintf(b, "- `R` %s %s\n", mdCode(c.Name), mdCode(shortDisp(c.From)))
		default:
			fmt.Fprintf(b, "- `M` %s %s → %s\n", mdCode(c.Name), mdCode(shortDisp(c.From)), mdCode(shortDisp(c.To)))
		}
	}
	b.WriteString("\n")
}

func shortDisp(s string) string {
	s = strings.ReplaceAll(s, "\n", "; ")
	if strings.HasPrefix(s, "sha256:") && len(s) > 7+16 {
		return s[:7+16]
	}
	return s
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
