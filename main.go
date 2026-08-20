package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	format := flag.String("format", "markdown", "output format: markdown or json")
	platform := flag.String("platform", "linux/amd64", "platform to inspect for multi-arch images")
	title := flag.String("title", "", "optional H1 title for markdown output")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: rpmdrift [flags] <from-image> <to-image>\n\n")
		fmt.Fprint(os.Stderr, "Diff the rpm package sets of two OCI images and print release notes.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *platform, *format, *title); err != nil {
		fmt.Fprintln(os.Stderr, "rpmdrift:", err)
		os.Exit(1)
	}
}

func run(fromRef, toRef, platform, format, title string) error {
	from, err := packagesFromImage(fromRef, platform)
	if err != nil {
		return fmt.Errorf("reading %s: %w", fromRef, err)
	}
	to, err := packagesFromImage(toRef, platform)
	if err != nil {
		return fmt.Errorf("reading %s: %w", toRef, err)
	}
	d := diff(from, to)
	switch format {
	case "markdown", "md":
		fmt.Print(renderMarkdown(d, title, fromRef, toRef))
	case "json":
		out, err := renderJSON(d)
		if err != nil {
			return err
		}
		fmt.Println(out)
	default:
		return fmt.Errorf("unknown format %q (want markdown or json)", format)
	}
	return nil
}
