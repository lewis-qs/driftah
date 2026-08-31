package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

type entry struct {
	name, link, body string
	typ              byte
}

func buildTar(t *testing.T, es ...entry) *tar.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range es {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Linkname: e.link, Size: int64(len(e.body))}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	return tar.NewReader(bytes.NewReader(buf.Bytes()))
}

func hh(s string) string { h := sha256.Sum256([]byte(s)); return "H:" + hex.EncodeToString(h[:]) }

// The ostree case: the db content is a sqlite-magic object in the object store,
// and the db path is a hardlink into it — resolution must follow the link.
func TestScanLayersDBResolve(t *testing.T) {
	obj := "sysroot/ostree/repo/objects/ab/cd.file"
	body := sqliteMagic + "rest-of-db"
	tr := buildTar(t,
		entry{name: obj, typ: tar.TypeReg, body: body},
		entry{name: "usr/share/rpm/rpmdb.sqlite", typ: tar.TypeLink, link: obj},
	)
	dbPath, _, _, _, err := scanLayers(tr, t.TempDir(), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if dbPath == "" {
		t.Fatal("db not resolved through hardlink")
	}
	got, _ := os.ReadFile(dbPath)
	if string(got) != body {
		t.Fatalf("db content = %q, want %q", got, body)
	}
}

func TestScanLayersFileIdentity(t *testing.T) {
	tr := buildTar(t,
		entry{name: "etc/foo.conf", typ: tar.TypeReg, body: "x"},
		entry{name: "usr/bin/tool", typ: tar.TypeLink, link: "sysroot/ostree/repo/objects/aa/bb.file"},
		entry{name: "usr/lib/link", typ: tar.TypeSymlink, link: "./target"},
		entry{name: "usr/lib64/mod.pyc", typ: tar.TypeReg, body: "bytecode"},
		entry{name: "usr/share/app/Packages", typ: tar.TypeReg, body: "notadb"},
		entry{name: "usr/lib/sysimage/rpm/rpmdb.sqlite", typ: tar.TypeReg, body: sqliteMagic + "db"},
		entry{name: "var/cache/x", typ: tar.TypeReg, body: "y"},
	)
	_, files, _, _, err := scanLayers(tr, t.TempDir(), []string{"etc/", "usr/"}, []string{"var/"}, true)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"etc/foo.conf":           hh("x"),
		"usr/bin/tool":           "L:sysroot/ostree/repo/objects/aa/bb.file",
		"usr/lib/link":           "S:target",   // path.Clean("./target")
		"usr/share/app/Packages": hh("notadb"), // basename "Packages" but not under a db dir -> kept
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v, want %d entries", files, len(want))
	}
	for k, v := range want {
		if files[k] != v {
			t.Errorf("%s = %q, want %q", k, files[k], v)
		}
	}
	// filtered: .pyc, the rpmdb under a db dir, and the var/ ignore prefix
	for _, bad := range []string{"usr/lib64/mod.pyc", "usr/lib/sysimage/rpm/rpmdb.sqlite", "var/cache/x"} {
		if _, ok := files[bad]; ok {
			t.Errorf("%s should be filtered out", bad)
		}
	}
}

func TestScanLayersApk(t *testing.T) {
	body := "P:busybox\nV:1.36.1-r15\nA:x86_64\n\nP:musl\nV:1.2.5-r1\nA:x86_64\n"
	tr := buildTar(t, entry{name: "lib/apk/db/installed", typ: tar.TypeReg, body: body})
	dbPath, _, _, pkgText, err := scanLayers(tr, t.TempDir(), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if dbPath != "" {
		t.Fatalf("unexpected rpm db %s", dbPath)
	}
	pkgs := map[string]Pkg{}
	mergePkgText(pkgs, pkgText)
	if pkgs["busybox.x86_64-1.36.1-r15"].Name != "busybox" || pkgs["musl.x86_64-1.2.5-r1"].Name != "musl" {
		t.Fatalf("pkgs = %v", pkgs)
	}
}

func TestScanLayersNoPkgDB(t *testing.T) {
	tr := buildTar(t, entry{name: "etc/foo", typ: tar.TypeReg, body: "x"})
	dbPath, files, _, pkgText, err := scanLayers(tr, t.TempDir(), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if dbPath != "" || len(pkgText) != 0 {
		t.Fatalf("dbPath=%q pkgText=%v", dbPath, pkgText)
	}
	if files["etc/foo"] != hh("x") {
		t.Fatalf("files = %v", files)
	}
}
