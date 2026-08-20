package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

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

func (p Pkg) nevraKey() string { return p.Name + "." + p.Arch + "-" + p.EVR() }

type imageData struct {
	pkgs  map[string]Pkg
	files map[string]string // path under a tracked prefix -> content identity
}

var (
	rpmDBDirs      = []string{"usr/lib/sysimage/rpm", "usr/share/rpm", "var/lib/rpm"}
	rpmDBMainFiles = []string{"rpmdb.sqlite", "Packages", "Packages.db"}
)

const sqliteMagic = "SQLite format 3\x00"

func isNoise(clean string) bool {
	base := path.Base(clean)
	return strings.HasSuffix(base, ".pyc") || slices.Contains(rpmDBMainFiles, base)
}

// In ostree/bootc images files are tar hardlinks into the content-addressed
// ostree object store, so the link target is a stable identity; regular files
// are hashed. The rpm db is found the same way: buffer any sqlite-magic file,
// then resolve the preferred db path through its hardlink.
func readImage(ref, platformStr string, prefixes, ignore []string, filterNoise bool) (*imageData, error) {
	plat, err := v1.ParsePlatform(platformStr)
	if err != nil {
		return nil, err
	}
	img, err := crane.Pull(ref, crane.WithPlatform(plat))
	if err != nil {
		return nil, fmt.Errorf("pull: %w", err)
	}

	dir, err := os.MkdirTemp("", "driftah-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	reg := map[string]string{}
	link := map[string]string{}
	files := map[string]string{}
	seq := 0

	pr, pw := io.Pipe()
	defer pr.Close()
	go func() { pw.CloseWithError(crane.Export(img, pw)) }()
	tr := tar.NewReader(pr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("export stream: %w", err)
		}
		clean := path.Clean(hdr.Name)
		isMain := slices.Contains(rpmDBMainFiles, path.Base(clean))
		tracked := hasAnyPrefix(clean, prefixes) && !hasAnyPrefix(clean, ignore)
		if tracked && filterNoise && isNoise(clean) {
			tracked = false
		}

		switch hdr.Typeflag {
		case tar.TypeLink:
			if isMain {
				link[clean] = path.Clean(hdr.Linkname)
			}
			if tracked {
				files[clean] = "L:" + path.Clean(hdr.Linkname)
			}
		case tar.TypeSymlink:
			if tracked {
				files[clean] = "S:" + hdr.Linkname
			}
		case tar.TypeReg:
			var head [16]byte
			n, rerr := io.ReadFull(tr, head[:])
			if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
				return nil, fmt.Errorf("read %s: %w", clean, rerr)
			}
			isSqlite := n >= len(sqliteMagic) && string(head[:len(sqliteMagic)]) == sqliteMagic
			needBuffer := isMain || isSqlite
			if !needBuffer && !tracked {
				continue
			}
			h := sha256.New()
			var out *os.File
			var ws []io.Writer
			if tracked {
				ws = append(ws, h)
			}
			if needBuffer {
				tmp := filepath.Join(dir, fmt.Sprintf("db%d", seq))
				seq++
				f, cerr := os.Create(tmp)
				if cerr != nil {
					return nil, cerr
				}
				out = f
				ws = append(ws, out)
				reg[clean] = tmp
			}
			mw := io.MultiWriter(ws...)
			_, werr := mw.Write(head[:n])
			if werr == nil {
				_, werr = io.Copy(mw, tr)
			}
			if out != nil {
				out.Close()
			}
			if werr != nil {
				return nil, fmt.Errorf("copy %s: %w", clean, werr)
			}
			if tracked {
				files[clean] = "H:" + hex.EncodeToString(h.Sum(nil))
			}
		}
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
	dbPath := ""
	for _, d := range rpmDBDirs {
		for _, f := range rpmDBMainFiles {
			if t := resolve(d+"/"+f, 0); t != "" {
				dbPath = t
				break
			}
		}
		if dbPath != "" {
			break
		}
	}
	if dbPath == "" {
		return nil, fmt.Errorf("no rpm database found in image")
	}

	db, err := rpmdb.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	list, err := db.ListPackages()
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
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
		pkgs[pk.nevraKey()] = pk
	}
	return &imageData{pkgs: pkgs, files: files}, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
