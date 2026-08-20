package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	rpmdb "github.com/knqyf263/go-rpmdb/pkg"

	_ "github.com/glebarez/go-sqlite"
)

type Pkg struct {
	Name    string
	Epoch   string
	Version string
	Release string
	Arch    string
}

func (p Pkg) EVR() string {
	if p.Epoch != "" && p.Epoch != "0" {
		return fmt.Sprintf("%s:%s-%s", p.Epoch, p.Version, p.Release)
	}
	return fmt.Sprintf("%s-%s", p.Version, p.Release)
}

func (p Pkg) key() string { return p.Name + "." + p.Arch }

var (
	rpmDBDirs      = []string{"usr/lib/sysimage/rpm", "usr/share/rpm", "var/lib/rpm"}
	rpmDBMainFiles = []string{"rpmdb.sqlite", "Packages", "Packages.db"}
)

const sqliteMagic = "SQLite format 3\x00"

func packagesFromImage(ref, platformStr string) (map[string]Pkg, error) {
	plat, err := v1.ParsePlatform(platformStr)
	if err != nil {
		return nil, err
	}
	img, err := crane.Pull(ref, crane.WithPlatform(plat))
	if err != nil {
		return nil, err
	}
	dbPath, dir, err := extractRPMDB(img)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	db, err := rpmdb.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	list, err := db.ListPackages()
	if err != nil {
		return nil, err
	}
	pkgs := make(map[string]Pkg, len(list))
	for _, p := range list {
		if p.Name == "gpg-pubkey" {
			continue
		}
		epoch := ""
		if p.Epoch != nil {
			epoch = fmt.Sprintf("%d", *p.Epoch)
		}
		pk := Pkg{Name: p.Name, Epoch: epoch, Version: p.Version, Release: p.Release, Arch: p.Arch}
		pkgs[pk.key()] = pk
	}
	return pkgs, nil
}

// extractRPMDB flattens the image and returns the path to the extracted rpm
// database file, preferring the authoritative current db. In ostree/bootc
// images the real content lives in the ostree object store under a content-hash
// name and the db paths are tar hardlinks into it, so buffer any file with the
// sqlite magic (plus any directly-named db file) and resolve the preferred path
// through its hardlink. ponytail: streams the whole flattened tar to grab a
// small db; add a per-layer reverse scan only if this is measurably too slow.
func extractRPMDB(img v1.Image) (dbFile, tmpDir string, err error) {
	dir, err := os.MkdirTemp("", "rpmdrift-")
	if err != nil {
		return "", "", err
	}
	reg := map[string]string{}  // clean archive path -> buffered temp file
	link := map[string]string{} // clean archive path -> clean hardlink target
	seq := 0

	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(crane.Export(img, pw)) }()
	tr := tar.NewReader(pr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(dir)
			return "", "", err
		}
		clean := path.Clean(hdr.Name)
		isMain := slices.Contains(rpmDBMainFiles, path.Base(clean))

		if hdr.Typeflag == tar.TypeLink && isMain {
			link[clean] = path.Clean(hdr.Linkname)
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		var head [16]byte
		n, err := io.ReadFull(tr, head[:])
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			os.RemoveAll(dir)
			return "", "", err
		}
		if !isMain && string(head[:n]) != sqliteMagic {
			continue
		}
		tmp := filepath.Join(dir, fmt.Sprintf("db%d", seq))
		seq++
		out, err := os.Create(tmp)
		if err != nil {
			os.RemoveAll(dir)
			return "", "", err
		}
		if _, err := out.Write(head[:n]); err != nil {
			out.Close()
			os.RemoveAll(dir)
			return "", "", err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			os.RemoveAll(dir)
			return "", "", err
		}
		out.Close()
		reg[clean] = tmp
	}

	var resolve func(p string, depth int) string
	resolve = func(p string, depth int) string {
		if depth > 10 {
			return ""
		}
		if t, ok := reg[p]; ok {
			return t
		}
		if tgt, ok := link[p]; ok {
			return resolve(tgt, depth+1)
		}
		return ""
	}
	for _, d := range rpmDBDirs {
		for _, f := range rpmDBMainFiles {
			if t := resolve(d+"/"+f, 0); t != "" {
				return t, dir, nil
			}
		}
	}
	os.RemoveAll(dir)
	return "", "", fmt.Errorf("no rpm database found in image")
}
