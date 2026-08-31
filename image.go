package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	rpmdb "github.com/knqyf263/go-rpmdb/pkg"

	_ "github.com/glebarez/go-sqlite"
)

const (
	pullTimeout = 10 * time.Minute
	sqliteMagic = "SQLite format 3\x00"
)

type Pkg struct {
	Name      string
	Epoch     string
	Version   string
	Release   string
	Arch      string
	Changelog string `json:"changelog,omitempty"`
}

func (p Pkg) EVR() string {
	v := p.Version
	if p.Release != "" {
		v += "-" + p.Release
	}
	if p.Epoch != "" && p.Epoch != "0" {
		return p.Epoch + ":" + v
	}
	return v
}

func (p Pkg) key() string { return p.Name + "." + p.Arch }

func (p Pkg) nevraKey() string { return p.Name + "." + p.Arch + "-" + p.EVR() }

type imageData struct {
	pkgs       map[string]Pkg
	files      map[string]string
	ignored    map[string]string
	owners     map[string]string // path -> package name
	changelogs map[string]string // package name -> first changelog stanza
}

var (
	rpmDBDirs      = []string{"usr/lib/sysimage/rpm", "usr/share/rpm", "var/lib/rpm"}
	rpmDBMainFiles = []string{"rpmdb.sqlite", "Packages", "Packages.db"}
)

func noiseKind(clean string) string {
	if strings.HasSuffix(path.Base(clean), ".pyc") {
		return "*.pyc"
	}
	if slices.Contains(rpmDBMainFiles, path.Base(clean)) && hasAnyPrefix(clean, rpmDBDirs) {
		return "rpm-db"
	}
	if hasAnyPrefix(clean, []string{"lib/apk/db/", "usr/lib/apk/db/"}) {
		return "apk-db"
	}
	if strings.HasPrefix(clean, "var/lib/dpkg/") {
		return "dpkg-db"
	}
	return ""
}

func isNoise(clean string) bool { return noiseKind(clean) != "" }

func readImage(ref, platformStr string, prefixes, ignore []string, filterNoise bool) (*imageData, error) {
	plat, err := v1.ParsePlatform(platformStr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	defer cancel()
	img, err := crane.Pull(ref, crane.WithPlatform(plat), crane.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("pull: %w", err)
	}

	dir, err := os.MkdirTemp("", "driftah-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	pr, pw := io.Pipe()
	defer pr.Close()
	go func() { pw.CloseWithError(crane.Export(img, pw)) }()
	dbPath, files, ignored, pkgText, err := scanLayers(tar.NewReader(pr), dir, prefixes, ignore, filterNoise)
	if err != nil {
		return nil, err
	}
	d := &imageData{
		pkgs: map[string]Pkg{}, files: files, ignored: ignored,
		owners: map[string]string{}, changelogs: map[string]string{},
	}
	if dbPath != "" {
		rpms, owners, rerr := readRPM(dbPath)
		if rerr != nil {
			return nil, rerr
		}
		d.pkgs, d.owners = rpms, owners
	}
	mergePkgText(d.pkgs, pkgText)
	mergeOwners(d, pkgText)
	mergeChangelogs(d, pkgText)
	attachChangelogs(d)
	return d, nil
}

func readRPM(dbPath string) (map[string]Pkg, map[string]string, error) {
	db, err := rpmdb.Open(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	list, err := db.ListPackages()
	if err != nil {
		return nil, nil, fmt.Errorf("list packages: %w", err)
	}
	pkgs := make(map[string]Pkg, len(list))
	owners := map[string]string{}
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
		names, _ := p.InstalledFileNames()
		for _, f := range names {
			owners[strings.TrimPrefix(path.Clean(f), "/")] = p.Name
		}
	}
	return pkgs, owners, nil
}

func mergePkgText(pkgs map[string]Pkg, files map[string][]byte) {
	for p, body := range files {
		extra, ok := pkgTextParse(p, body)
		if !ok {
			continue
		}
		for k, v := range extra {
			pkgs[k] = v
		}
	}
}

func isPkgText(p string) bool {
	if _, ok := pkgTextParse(p, nil); ok {
		return true
	}
	if _, ok := changelogPkg(p); ok {
		return true
	}
	_, ok := dpkgListPkg(p)
	return ok
}

func changelogPkg(p string) (string, bool) {
	const pre = "usr/share/doc/"
	if !strings.HasPrefix(p, pre) {
		return "", false
	}
	rest := p[len(pre):]
	i := strings.IndexByte(rest, '/')
	if i < 1 {
		return "", false
	}
	name, base := rest[:i], rest[i+1:]
	switch base {
	case "changelog.Debian.gz", "changelog.Debian", "changelog.gz", "changelog", "ChangeLog":
		return name, true
	}
	return "", false
}

func mergeChangelogs(d *imageData, pkgText map[string][]byte) {
	if d.changelogs == nil {
		d.changelogs = map[string]string{}
	}
	for p, body := range pkgText {
		pkg, ok := changelogPkg(p)
		if !ok {
			continue
		}
		if cl := firstChangelog(body); cl != "" {
			d.changelogs[pkg] = cl
		}
	}
}

func attachChangelogs(d *imageData) {
	for k, p := range d.pkgs {
		if cl := d.changelogs[p.Name]; cl != "" {
			p.Changelog = cl
			d.pkgs[k] = p
		}
	}
}

func firstChangelog(b []byte) string {
	if isGzip(b) {
		plain, err := gunzip(b)
		if err == nil {
			b = plain
		}
	}
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, " -- ") && len(out) > 0 {
			break
		}
		out = append(out, ln)
		if len(out) >= 8 {
			break
		}
	}
	s := strings.TrimSpace(strings.Join(out, "\n"))
	if len(s) > 1024 {
		s = s[:1024]
	}
	return s
}

func dpkgListPkg(p string) (string, bool) {
	const pre = "var/lib/dpkg/info/"
	if !strings.HasPrefix(p, pre) || !strings.HasSuffix(p, ".list") {
		return "", false
	}
	base := strings.TrimSuffix(p[len(pre):], ".list")
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	return base, base != ""
}

func mergeOwners(d *imageData, pkgText map[string][]byte) {
	if d.owners == nil {
		d.owners = map[string]string{}
	}
	for p, body := range pkgText {
		switch p {
		case "lib/apk/db/installed", "usr/lib/apk/db/installed":
			_, own := parseApkBoth(body)
			for k, v := range own {
				d.owners[k] = v
			}
		}
		if pkg, ok := dpkgListPkg(p); ok {
			for _, line := range strings.Split(string(body), "\n") {
				line = strings.TrimPrefix(strings.TrimSpace(line), "/")
				if line != "" {
					d.owners[path.Clean(line)] = pkg
				}
			}
		}
	}
}

// scanLayers reads a flattened image tar, buffering rpm/apk/dpkg databases and
// a content-identity map for files under the tracked prefixes. Ostree stores
// the rpm db in the object store; /usr paths are hardlinks into it.

func scanLayers(tr *tar.Reader, dir string, prefixes, ignore []string, filterNoise bool) (string, map[string]string, map[string]string, map[string][]byte, error) {
	reg := map[string]string{}
	link := map[string]string{}
	files := map[string]string{}
	ignored := map[string]string{}
	pkgText := map[string][]byte{}
	seq := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("export stream: %w", err)
		}
		clean := path.Clean(hdr.Name)
		isMain := slices.Contains(rpmDBMainFiles, path.Base(clean))
		underPrefix := hasAnyPrefix(clean, prefixes)
		tracked := underPrefix && !hasAnyPrefix(clean, ignore)
		if tracked && filterNoise && isNoise(clean) {
			tracked = false
		}
		// under a scanned prefix but excluded by --ignore or the noise filter:
		// still record its identity so we can report a count of ignored changes
		filtered := underPrefix && !tracked

		switch hdr.Typeflag {
		case tar.TypeLink:
			if isMain {
				link[clean] = path.Clean(hdr.Linkname)
			}
			if tracked {
				files[clean] = "L:" + path.Clean(hdr.Linkname)
			} else if filtered {
				ignored[clean] = "L:" + path.Clean(hdr.Linkname)
			}
		case tar.TypeSymlink:
			if tracked {
				files[clean] = "S:" + path.Clean(hdr.Linkname)
			} else if filtered {
				ignored[clean] = "S:" + path.Clean(hdr.Linkname)
			}
		case tar.TypeReg:
			var head [16]byte
			n, rerr := io.ReadFull(tr, head[:])
			if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
				return "", nil, nil, nil, fmt.Errorf("read %s: %w", clean, rerr)
			}
			isSqlite := n >= len(sqliteMagic) && string(head[:len(sqliteMagic)]) == sqliteMagic
			needBuffer := isMain || isSqlite
			needPkgText := isPkgText(clean)
			if !needBuffer && !needPkgText && !tracked && !filtered {
				continue
			}
			h := sha256.New()
			var out *os.File
			var extra bytes.Buffer
			var ws []io.Writer
			if tracked || filtered {
				ws = append(ws, h)
			}
			if needBuffer {
				tmp := filepath.Join(dir, fmt.Sprintf("db%d", seq))
				seq++
				f, cerr := os.Create(tmp)
				if cerr != nil {
					return "", nil, nil, nil, cerr
				}
				out = f
				ws = append(ws, out)
				reg[clean] = tmp
			}
			if needPkgText {
				ws = append(ws, &extra)
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
				return "", nil, nil, nil, fmt.Errorf("copy %s: %w", clean, werr)
			}
			if needPkgText {
				pkgText[clean] = append([]byte(nil), extra.Bytes()...)
			}
			if tracked {
				files[clean] = "H:" + hex.EncodeToString(h.Sum(nil))
			} else if filtered {
				ignored[clean] = "H:" + hex.EncodeToString(h.Sum(nil))
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
	for _, d := range rpmDBDirs {
		for _, f := range rpmDBMainFiles {
			if t := resolve(d+"/"+f, 0); t != "" {
				return t, files, ignored, pkgText, nil
			}
		}
	}
	return "", files, ignored, pkgText, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
