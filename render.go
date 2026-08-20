package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func renderMarkdown(d Diff, title, fromRef, toRef string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	fmt.Fprintf(&b, "**From:** `%s`  \n**To:** `%s`\n\n", fromRef, toRef)

	if d.empty() {
		b.WriteString("No package changes.\n")
		return b.String()
	}

	if len(d.Updated) > 0 {
		fmt.Fprintf(&b, "### Updated packages (%d)\n\n", len(d.Updated))
		for _, u := range d.Updated {
			fmt.Fprintf(&b, "- **%s** `%s` → `%s`\n", u.Name, u.From, u.To)
		}
		b.WriteString("\n")
	}
	if len(d.Added) > 0 {
		fmt.Fprintf(&b, "### Added packages (%d)\n\n", len(d.Added))
		for _, p := range d.Added {
			fmt.Fprintf(&b, "- %s `%s`\n", p.Name, p.EVR())
		}
		b.WriteString("\n")
	}
	if len(d.Removed) > 0 {
		fmt.Fprintf(&b, "### Removed packages (%d)\n\n", len(d.Removed))
		for _, p := range d.Removed {
			fmt.Fprintf(&b, "- %s `%s`\n", p.Name, p.EVR())
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderJSON(d Diff) (string, error) {
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
