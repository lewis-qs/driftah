package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

var version = "dev"

type options struct {
	platform, format, title string
	prefixes, ignore        []string
	ignorePkgs, highlight   []string
	failOn                  []string
	filterNoise             bool
	shortVersions           bool
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	format := flag.String("format", "markdown", "output format: markdown or json")
	platform := flag.String("platform", "linux/amd64", "platform to inspect for multi-arch images")
	title := flag.String("title", "", "optional H1 title for markdown output")
	paths := flag.String("paths", "etc/,usr/", "comma-separated path prefixes to diff (empty to skip the file diff)")
	ignore := flag.String("ignore", "", "comma-separated path prefixes to omit from the file diff")
	ignorePkgs := flag.String("ignore-packages", "", "comma-separated package families to omit (e.g. kernel)")
	highlight := flag.String("highlight", "", "comma-separated packages to list current versions for (Key versions section)")
	shortVersions := flag.Bool("short-versions", false, "in the Key versions section, show only the upstream version (drop epoch and release)")
	noFilter := flag.Bool("no-filter", false, "keep noisy files (*.pyc, rpm db) in the file diff")
	failOn := flag.String("fail-on", "", "exit 1 after printing if any of: files, highlight, or path prefixes (e.g. etc/,highlight)")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: driftah [flags] <from> <to>\n\n")
		fmt.Fprint(os.Stderr, "Diff two OCI images, systemd UKIs (.efi), or disk images (.img) and print rpm/apk/deb and file release notes.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("driftah", version)
		return
	}
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	o := options{
		platform:      *platform,
		format:        *format,
		title:         *title,
		prefixes:      parsePrefixes(*paths),
		ignore:        parsePrefixes(*ignore),
		ignorePkgs:    parseCSV(*ignorePkgs),
		highlight:     parseCSV(*highlight),
		failOn:        parseCSV(*failOn),
		filterNoise:   !*noFilter,
		shortVersions: *shortVersions,
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
	KeyVersions []KeyVersion    `json:"key_versions"`
	UKI         *UKIReport      `json:"uki,omitempty"`
	Packages    Diff            `json:"packages"`
	Files       FileDiff        `json:"files"`
	Ignored     []IgnoredBucket `json:"ignored"`
}

func run(fromRef, toRef string, o options) error {
	from, fromUKI, err := readInput(fromRef, o.platform, o.prefixes, o.ignore, o.filterNoise)
	if err != nil {
		return fmt.Errorf("reading %s: %w", fromRef, err)
	}
	to, toUKI, err := readInput(toRef, o.platform, o.prefixes, o.ignore, o.filterNoise)
	if err != nil {
		return fmt.Errorf("reading %s: %w", toRef, err)
	}
	rep := Report{
		KeyVersions: keyVersions(to.pkgs, o.highlight, o.shortVersions),
		UKI:         ukiDiff(fromUKI, toUKI),
		Packages:    diff(dropPkgs(from.pkgs, o.ignorePkgs), dropPkgs(to.pkgs, o.ignorePkgs)),
		Files:       fileDiff(from.files, to.files, mergeOwnerMaps(from.owners, to.owners), o.prefixes),
		Ignored:     ignoredChanges(from.ignored, to.ignored, o.ignore, o.filterNoise),
	}
	switch o.format {
	case "markdown":
		fmt.Print(renderMarkdown(rep, o.title, fromRef, toRef))
	case "json":
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	default:
		return fmt.Errorf("unknown format %q (want markdown or json)", o.format)
	}
	if shouldFail(rep, o.failOn, o.highlight) {
		os.Exit(1)
	}
	return nil
}

func shouldFail(rep Report, failOn, highlight []string) bool {
	for _, tok := range failOn {
		switch tok {
		case "files":
			if !rep.Files.empty() {
				return true
			}
		case "highlight":
			if highlightMoved(rep.Packages, highlight) {
				return true
			}
		default:
			pre := strings.TrimPrefix(tok, "/")
			if !strings.HasSuffix(pre, "/") {
				pre += "/"
			}
			for _, g := range rep.Files.Groups {
				if g.Prefix == pre && len(g.Changes) > 0 {
					return true
				}
			}
		}
	}
	return false
}

func highlightMoved(d Diff, names []string) bool {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, u := range d.Updated {
		if want[u.Name] {
			return true
		}
	}
	for _, p := range append(d.Added, d.Removed...) {
		if want[p.Name] {
			return true
		}
	}
	return false
}
