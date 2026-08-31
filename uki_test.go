package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type peIn struct {
	Name string
	Body []byte
}

func makePE(signed bool, secs ...peIn) []byte {
	const dos, optSize = 64, 0xF0
	nsec := len(secs)
	headers := dos + 4 + 20 + optSize + nsec*40
	hdrPad := (headers + 511) &^ 511
	buf := make([]byte, hdrPad)
	buf[0], buf[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(buf[0x3C:], dos)
	copy(buf[dos:], "PE\x00\x00")
	coff := dos + 4
	binary.LittleEndian.PutUint16(buf[coff:], 0x8664)
	binary.LittleEndian.PutUint16(buf[coff+2:], uint16(nsec))
	binary.LittleEndian.PutUint16(buf[coff+16:], optSize)
	opt := coff + 20
	binary.LittleEndian.PutUint16(buf[opt:], 0x20B)
	binary.LittleEndian.PutUint32(buf[opt+108:], 16)
	off := hdrPad
	for i, s := range secs {
		h := opt + optSize + i*40
		name := s.Name
		if len(name) > 8 {
			name = name[:8]
		}
		copy(buf[h:], name)
		binary.LittleEndian.PutUint32(buf[h+8:], uint32(len(s.Body)))
		raw := (len(s.Body) + 511) &^ 511
		if len(s.Body) == 0 {
			raw = 0
		}
		binary.LittleEndian.PutUint32(buf[h+16:], uint32(raw))
		if raw > 0 {
			binary.LittleEndian.PutUint32(buf[h+20:], uint32(off))
			pad := make([]byte, raw)
			copy(pad, s.Body)
			buf = append(buf, pad...)
			off += raw
		}
	}
	if signed {
		// Authenticode overlay: WIN_CERTIFICATE after the image, 8-aligned.
		// Data directory 4 (Certificate Table) uses a file offset, not an RVA.
		for len(buf)%8 != 0 {
			buf = append(buf, 0)
		}
		certOff := len(buf)
		cert := make([]byte, 72)
		binary.LittleEndian.PutUint32(cert[0:], uint32(len(cert)))
		binary.LittleEndian.PutUint16(cert[4:], 0x200)
		binary.LittleEndian.PutUint16(cert[6:], 0x0002)
		copy(cert[8:], []byte("dummy-authenticode"))
		buf = append(buf, cert...)
		dir := opt + 112 + 4*8
		binary.LittleEndian.PutUint32(buf[dir:], uint32(certOff))
		binary.LittleEndian.PutUint32(buf[dir+4:], uint32(len(cert)))
	}
	return buf
}

func newc(name string, body []byte, mode uint32) []byte {
	namesize := len(name) + 1
	hdr := fmt.Sprintf("070701%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X",
		0, mode, 0, 0, 1, 0, len(body), 0, 0, 0, 0, namesize, 0)
	buf := append([]byte(hdr), name...)
	buf = append(buf, 0)
	for len(buf)%4 != 0 {
		buf = append(buf, 0)
	}
	buf = append(buf, body...)
	for len(buf)%4 != 0 {
		buf = append(buf, 0)
	}
	return buf
}

func cpioGz(ents ...[]byte) []byte {
	var all []byte
	for _, e := range ents {
		all = append(all, e...)
	}
	all = append(all, newc("TRAILER!!!", nil, 0)...)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(all)
	_ = gz.Close()
	return buf.Bytes()
}

const modeReg = 0x8000 | 0644

func TestPESectionsAndUKI(t *testing.T) {
	pe := makePE(true,
		peIn{".osrel", []byte("NAME=Alpine Linux\nID=alpine\nVERSION_ID=3.21\n")},
		peIn{".cmdline", []byte("console=ttyS0")},
		peIn{".linux", []byte("KERNEL")},
		peIn{".initrd", cpioGz(newc("etc/foo", []byte("bar"), modeReg))},
	)
	if !isUKI(pe) {
		t.Fatal("expected UKI")
	}
	secs, signed, err := peSections(pe)
	if err != nil {
		t.Fatal(err)
	}
	if !signed {
		t.Fatal("expected signed")
	}
	if string(secs[".linux"]) != "KERNEL" {
		t.Fatalf("linux = %q", secs[".linux"])
	}
	if !strings.Contains(string(secs[".osrel"]), "ID=alpine") {
		t.Fatalf("osrel = %q", secs[".osrel"])
	}
}

func TestParseUKIInitrdAndApk(t *testing.T) {
	apk := []byte("P:busybox\nV:1.36.1-r15\nA:x86_64\nF:etc\nR:foo\n\nP:musl\nV:1.2.5-r1\nA:x86_64\n")
	initrd := cpioGz(
		newc("etc/foo", []byte("v1"), modeReg),
		newc("lib/apk/db/installed", apk, modeReg),
		newc("bin/busybox", []byte("bb"), modeReg),
	)
	pe := makePE(false,
		peIn{".osrel", []byte("ID=alpine\n")},
		peIn{".cmdline", []byte("root=/dev/ram0")},
		peIn{".linux", []byte("k")},
		peIn{".initrd", initrd},
	)
	d, meta, err := parseUKI(pe, []string{"etc/", "usr/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Cmdline != "root=/dev/ram0" || meta.Arch != "x86_64" || meta.Signed {
		t.Fatalf("meta = %+v", meta)
	}
	if d.files["etc/foo"] != hh("v1") {
		t.Fatalf("files = %v", d.files)
	}
	if _, ok := d.files["bin/busybox"]; ok {
		t.Fatal("bin/busybox should be outside prefixes")
	}
	if d.pkgs["busybox.x86_64-1.36.1-r15"].Name != "busybox" {
		t.Fatalf("pkgs = %v", d.pkgs)
	}
	if d.owners["etc/foo"] != "busybox" {
		t.Fatalf("owners = %v", d.owners)
	}
}

func TestParseUKIDpkg(t *testing.T) {
	status := []byte("Package: apt\nStatus: install ok installed\nArchitecture: amd64\nVersion: 3.0.3\n\n")
	pe := makePE(false,
		peIn{".linux", []byte("k")},
		peIn{".initrd", cpioGz(newc("var/lib/dpkg/status", status, modeReg))},
	)
	d, _, err := parseUKI(pe, []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if d.pkgs["apt.amd64-3.0.3"].Name != "apt" {
		t.Fatalf("pkgs = %v", d.pkgs)
	}
}

func TestPickUKIFromDisk(t *testing.T) {
	uki := makePE(false,
		peIn{".osrel", []byte("VERSION_ID=3.21\n")},
		peIn{".linux", []byte("k3")},
		peIn{".initrd", []byte{0x1f, 0x8b}},
	)
	old := makePE(false,
		peIn{".osrel", []byte("VERSION_ID=3.20\n")},
		peIn{".linux", []byte("k1")},
		peIn{".initrd", []byte{0x1f, 0x8b}},
	)
	disk := make([]byte, 4096)
	copy(disk[512:], old)
	disk = append(disk, make([]byte, 512)...)
	disk = append(disk, uki...)
	got := pickUKI(disk)
	if peText(mustSecs(t, got)[".osrel"]) != "VERSION_ID=3.21" {
		t.Fatalf("picked wrong UKI")
	}
}

func mustSecs(t *testing.T, pe []byte) map[string][]byte {
	t.Helper()
	secs, _, err := peSections(pe)
	if err != nil {
		t.Fatal(err)
	}
	return secs
}

func TestParseDpkgStatus(t *testing.T) {
	got := parseDpkg([]byte("Package: apt\nStatus: install ok installed\nArchitecture: amd64\nVersion: 3.0.3\n\nPackage: gone\nStatus: deinstall ok config-files\nArchitecture: amd64\nVersion: 1\n"))
	if len(got) != 1 || got["apt.amd64-3.0.3"].Version != "3.0.3" {
		t.Fatalf("got %v", got)
	}
}

func TestUKIDiffAndRender(t *testing.T) {
	from := makePE(false,
		peIn{".osrel", []byte("ID=alpine\nVERSION_ID=3.20\n")},
		peIn{".cmdline", []byte("console=ttyS0 quiet")},
		peIn{".linux", []byte("k")},
		peIn{".initrd", cpioGz(newc("etc/a", []byte("1"), modeReg))},
	)
	to := makePE(false,
		peIn{".osrel", []byte("ID=alpine\nVERSION_ID=3.21\n")},
		peIn{".cmdline", []byte("console=ttyS0 quiet")},
		peIn{".linux", []byte("k")},
		peIn{".initrd", cpioGz(newc("etc/a", []byte("2"), modeReg))},
	)
	fd, fu, err := parseUKI(from, []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	td, tu, err := parseUKI(to, []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	u := ukiDiff(fu, tu)
	if u.empty() {
		t.Fatal("expected UKI section changes")
	}
	fdFiles := fileDiff(fd.files, td.files, td.owners, []string{"etc/"})
	if fdFiles.empty() {
		t.Fatal("expected file change in etc/a")
	}
	md := renderMarkdown(Report{UKI: u, Files: fdFiles, Packages: Diff{}}, "", "from.efi", "to.efi")
	for _, want := range []string{"### UKI", "VERSION_ID=3.21", "### UKI sections", "### Changed files in /etc"} {
		if !bytes.Contains([]byte(md), []byte(want)) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestUKISectionSizes(t *testing.T) {
	from := makePE(false,
		peIn{".linux", bytes.Repeat([]byte("k"), 2000)},
		peIn{".initrd", bytes.Repeat([]byte("i"), 3000)},
	)
	to := makePE(false,
		peIn{".linux", bytes.Repeat([]byte("K"), 8000)},
		peIn{".initrd", bytes.Repeat([]byte("I"), 9000)},
	)
	_, fu, err := parseUKI(from, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	_, tu, err := parseUKI(to, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	md := renderMarkdown(Report{UKI: ukiDiff(fu, tu), Packages: Diff{}}, "", "a.efi", "b.efi")
	if !strings.Contains(md, "→") || !strings.Contains(md, "K") {
		t.Fatalf("expected size note:\n%s", md)
	}
	if !strings.Contains(md, ".linux") {
		t.Fatalf("missing .linux:\n%s", md)
	}
}

func TestReadUKIFileAndImageScan(t *testing.T) {
	uki := makePE(false,
		peIn{".osrel", []byte("ID=alpine\n")},
		peIn{".cmdline", []byte("console=ttyS0")},
		peIn{".linux", []byte("k")},
		peIn{".initrd", cpioGz(newc("etc/x", []byte("y"), modeReg))},
	)
	dir := t.TempDir()
	efi := filepath.Join(dir, "app.efi")
	img := filepath.Join(dir, "app.img")
	if err := os.WriteFile(efi, uki, 0o644); err != nil {
		t.Fatal(err)
	}
	disk := make([]byte, 2048)
	disk = append(disk, uki...)
	if err := os.WriteFile(img, disk, 0o644); err != nil {
		t.Fatal(err)
	}
	d1, m1, err := readUKI(efi, []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	d2, m2, err := readUKI(img, []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Cmdline != m2.Cmdline || d1.files["etc/x"] != d2.files["etc/x"] {
		t.Fatalf("efi vs img mismatch: %+v %+v %v %v", m1, m2, d1.files, d2.files)
	}
}

func ukiSecs(initrd []byte) []peIn {
	return []peIn{
		{".osrel", []byte("ID=alpine\nVERSION_ID=3.21\n")},
		{".cmdline", []byte("console=ttyS0 quiet")},
		{".linux", []byte("KERNEL")},
		{".initrd", initrd},
	}
}

func TestSignedUKIReadable(t *testing.T) {
	initrd := cpioGz(newc("etc/foo", []byte("secret"), modeReg))
	d, meta, err := parseUKI(makePE(true, ukiSecs(initrd)...), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Signed {
		t.Fatal("expected Authenticode overlay to count as signed")
	}
	if meta.Cmdline != "console=ttyS0 quiet" {
		t.Fatalf("cmdline = %q", meta.Cmdline)
	}
	if d.files["etc/foo"] != hh("secret") {
		t.Fatalf("initrd files = %v", d.files)
	}
}

func TestSignDoesNotHidePayload(t *testing.T) {
	initrd := cpioGz(newc("etc/foo", []byte("secret"), modeReg))
	secs := ukiSecs(initrd)
	_, unsigned, err := parseUKI(makePE(false, secs...), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	d, signed, err := parseUKI(makePE(true, secs...), []string{"etc/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if unsigned.Signed || !signed.Signed {
		t.Fatalf("signed flags unsigned=%v signed=%v", unsigned.Signed, signed.Signed)
	}
	if unsigned.Cmdline != signed.Cmdline || unsigned.OSRel != signed.OSRel {
		t.Fatal("signing changed text sections")
	}
	if unsigned.Sections[".linux"].Display != signed.Sections[".linux"].Display {
		t.Fatal("signing changed .linux hash")
	}
	if unsigned.Sections[".initrd"].Display != signed.Sections[".initrd"].Display {
		t.Fatal("signing changed .initrd hash")
	}
	if d.files["etc/foo"] != hh("secret") {
		t.Fatalf("files = %v", d.files)
	}
	rep := ukiDiff(unsigned, signed)
	for _, c := range rep.Changes {
		if c.Name == ".linux" || c.Name == ".initrd" || c.Name == ".cmdline" || c.Name == ".osrel" {
			t.Fatalf("payload section %s should be unchanged: %+v", c.Name, c)
		}
	}
}
