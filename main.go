package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	format := flag.String("format", "markdown", "output format: markdown or json")
	platform := flag.String("platform", "linux/amd64", "platform to inspect for multi-arch images")
	title := flag.String("title", "", "optional H1 title for markdown output")
	paths := flag.String("paths", "etc/,usr/", "comma-separated path prefixes to diff (empty to skip the file diff)")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: driftah [flags] <from-image> <to-image>\n\n")
		fmt.Fprint(os.Stderr, "Diff the rpm package sets of two OCI images and print release notes.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *platform, *format, *title, parsePrefixes(*paths)); err != nil {
		fmt.Fprintln(os.Stderr, "driftah:", err)
		os.Exit(1)
	}
}

func parsePrefixes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimPrefix(strings.TrimSpace(p), "/")
		if p == "" {
			continue
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		out = append(out, p)
	}
	return out
}

type Report struct {
	Packages Diff     `json:"packages"`
	Files    FileDiff `json:"files"`
}

func run(fromRef, toRef, platform, format, title string, prefixes []string) error {
	from, err := readImage(fromRef, platform, prefixes)
	if err != nil {
		return fmt.Errorf("reading %s: %w", fromRef, err)
	}
	to, err := readImage(toRef, platform, prefixes)
	if err != nil {
		return fmt.Errorf("reading %s: %w", toRef, err)
	}
	rep := Report{
		Packages: diff(from.pkgs, to.pkgs),
		Files:    fileDiff(from.files, to.files, prefixes),
	}
	switch format {
	case "markdown", "md":
		fmt.Print(renderMarkdown(rep, title, fromRef, toRef))
	case "json":
		out, err := renderJSON(rep)
		if err != nil {
			return err
		}
		fmt.Println(out)
	default:
		return fmt.Errorf("unknown format %q (want markdown or json)", format)
	}
	return nil
}
