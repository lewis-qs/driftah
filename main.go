package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type options struct {
	platform, format, title string
	prefixes, ignore        []string
	ignorePkgs              []string
	filterNoise             bool
}

func main() {
	format := flag.String("format", "markdown", "output format: markdown or json")
	platform := flag.String("platform", "linux/amd64", "platform to inspect for multi-arch images")
	title := flag.String("title", "", "optional H1 title for markdown output")
	paths := flag.String("paths", "etc/,usr/", "comma-separated path prefixes to diff (empty to skip the file diff)")
	ignore := flag.String("ignore", "", "comma-separated path prefixes to omit from the file diff")
	ignorePkgs := flag.String("ignore-packages", "", "comma-separated package families to omit (e.g. kernel)")
	noFilter := flag.Bool("no-filter", false, "keep noisy files (*.pyc, rpm db) in the file diff")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: driftah [flags] <from-image> <to-image>\n\n")
		fmt.Fprint(os.Stderr, "Diff two OCI images and print package and file release notes.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	o := options{
		platform:    *platform,
		format:      *format,
		title:       *title,
		prefixes:    parsePrefixes(*paths),
		ignore:      parsePrefixes(*ignore),
		ignorePkgs:  parseCSV(*ignorePkgs),
		filterNoise: !*noFilter,
	}
	if err := run(flag.Arg(0), flag.Arg(1), o); err != nil {
		fmt.Fprintln(os.Stderr, "driftah:", err)
		os.Exit(1)
	}
}

func parsePrefixes(s string) []string {
	var out []string
	for _, p := range parseCSV(s) {
		p = strings.TrimPrefix(p, "/")
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		out = append(out, p)
	}
	return out
}

func parseCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type Report struct {
	Packages Diff     `json:"packages"`
	Files    FileDiff `json:"files"`
}

func run(fromRef, toRef string, o options) error {
	from, err := readImage(fromRef, o.platform, o.prefixes, o.ignore, o.filterNoise)
	if err != nil {
		return fmt.Errorf("reading %s: %w", fromRef, err)
	}
	to, err := readImage(toRef, o.platform, o.prefixes, o.ignore, o.filterNoise)
	if err != nil {
		return fmt.Errorf("reading %s: %w", toRef, err)
	}
	rep := Report{
		Packages: diff(dropPkgs(from.pkgs, o.ignorePkgs), dropPkgs(to.pkgs, o.ignorePkgs)),
		Files:    fileDiff(from.files, to.files, o.prefixes),
	}
	switch o.format {
	case "markdown", "md":
		fmt.Print(renderMarkdown(rep, o.title, fromRef, toRef))
	case "json":
		out, err := renderJSON(rep)
		if err != nil {
			return err
		}
		fmt.Println(out)
	default:
		return fmt.Errorf("unknown format %q (want markdown or json)", o.format)
	}
	return nil
}
