package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const maxUKIBytes = 512 << 20

type ukiSec struct {
	Size    int
	Display string // text (NUL-stripped) or sha256:<hex>
}

type ukiMeta struct {
	Arch     string
	Signed   bool
	Cmdline  string
	OSRel    string
	Sections map[string]ukiSec
}

type UKISectionChange struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	FromSize int    `json:"from_size,omitempty"`
	ToSize   int    `json:"to_size,omitempty"`
}

type UKIReport struct {
	Arch    string             `json:"arch"`
	Signed  bool               `json:"signed"`
	Cmdline string             `json:"cmdline"`
	OSRel   string             `json:"os_release"`
	Changes []UKISectionChange `json:"changes"`
}

var ukiTextSections = map[string]bool{
	".cmdline": true,
	".osrel":   true,
	".sbat":    true,
	".sdmagic": true,
}

func isLocalFile(ref string) bool {
	st, err := os.Stat(ref)
	return err == nil && st.Mode().IsRegular()
}

func readInput(ref, platform string, prefixes, ignore []string, filterNoise bool) (*imageData, *ukiMeta, error) {
	if isLocalFile(ref) {
		return readUKI(ref, prefixes, ignore, filterNoise)
	}
	d, err := readImage(ref, platform, prefixes, ignore, filterNoise)
	return d, nil, err
}

func readUKI(path string, prefixes, ignore []string, filterNoise bool) (*imageData, *ukiMeta, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if st.Size() > maxUKIBytes {
		return nil, nil, fmt.Errorf("%s is %d bytes (max %d); not scanning", path, st.Size(), maxUKIBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	pe := pickUKI(raw)
	if pe == nil {
		return nil, nil, fmt.Errorf("%s is not a UKI (PE with .linux+.initrd) or a disk image containing one", path)
	}
	return parseUKI(pe, prefixes, ignore, filterNoise)
}

func pickUKI(raw []byte) []byte {
	if isUKI(raw) {
		return raw
	}
	// Scan for a PE with .linux+.initrd (systemd UKI). Last hit wins if several.
	var best []byte
	for off := 0; off+64 < len(raw); off += 512 {
		if raw[off] != 'M' || raw[off+1] != 'Z' {
			continue
		}
		slice := raw[off:]
		if isUKI(slice) {
			best = slice
		}
	}
	return best
}

func isUKI(data []byte) bool {
	secs, _, err := peSections(data)
	if err != nil {
		return false
	}
	_, linux := secs[".linux"]
	_, initrd := secs[".initrd"]
	return linux && initrd
}

func parseUKI(pe []byte, prefixes, ignore []string, filterNoise bool) (*imageData, *ukiMeta, error) {
	secs, signed, err := peSections(pe)
	if err != nil {
		return nil, nil, err
	}
	machine := peMachine(pe)
	meta := &ukiMeta{
		Arch:     machine,
		Signed:   signed,
		Cmdline:  peText(secs[".cmdline"]),
		OSRel:    peText(secs[".osrel"]),
		Sections: map[string]ukiSec{},
	}
	for name, body := range secs {
		s := ukiSec{Size: len(body)}
		if ukiTextSections[name] {
			s.Display = peText(body)
		} else {
			sum := sha256.Sum256(body)
			s.Display = "sha256:" + hex.EncodeToString(sum[:])
		}
		meta.Sections[name] = s
	}

	d := &imageData{pkgs: map[string]Pkg{}, files: map[string]string{}, ignored: map[string]string{}}
	if initrd := secs[".initrd"]; len(initrd) > 0 {
		if err := walkInitrd(initrd, d, prefixes, ignore, filterNoise); err != nil {
			return nil, nil, fmt.Errorf("initrd: %w", err)
		}
	}
	return d, meta, nil
}

func peText(b []byte) string {
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", ""))
}

func peMachine(data []byte) string {
	if len(data) < 0x40 {
		return "unknown"
	}
	off := int(binary.LittleEndian.Uint32(data[0x3C:]))
	if off+6 > len(data) {
		return "unknown"
	}
	switch binary.LittleEndian.Uint16(data[off+4:]) {
	case 0x8664:
		return "x86_64"
	case 0xAA64:
		return "aarch64"
	default:
		return fmt.Sprintf("0x%x", binary.LittleEndian.Uint16(data[off+4:]))
	}
}

func peSections(data []byte) (map[string][]byte, bool, error) {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return nil, false, fmt.Errorf("not a PE")
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3C:]))
	if peOff < 0 || peOff+24 > len(data) || string(data[peOff:peOff+4]) != "PE\x00\x00" {
		return nil, false, fmt.Errorf("invalid PE signature")
	}
	coff := peOff + 4
	nsec := int(binary.LittleEndian.Uint16(data[coff+2:]))
	optSize := int(binary.LittleEndian.Uint16(data[coff+16:]))
	opt := coff + 20
	if nsec < 1 || nsec > 64 || optSize < 2 || opt+optSize > len(data) {
		return nil, false, fmt.Errorf("implausible PE headers")
	}
	magic := binary.LittleEndian.Uint16(data[opt:])
	var ddOff int
	switch magic {
	case 0x20B:
		ddOff = opt + 112
	case 0x10B:
		ddOff = opt + 96
	default:
		return nil, false, fmt.Errorf("unknown optional-header magic %#x", magic)
	}
	signed := false
	if ddOff+4 <= len(data) {
		numRva := int(binary.LittleEndian.Uint32(data[ddOff-4:]))
		if numRva > 4 {
			entry := ddOff + 4*8
			if entry+8 <= len(data) {
				signed = binary.LittleEndian.Uint32(data[entry+4:]) > 0
			}
		}
	}
	secOff := opt + optSize
	out := map[string][]byte{}
	for i := 0; i < nsec; i++ {
		off := secOff + i*40
		if off+40 > len(data) {
			return nil, false, fmt.Errorf("section header %d out of bounds", i)
		}
		name := string(bytes.TrimRight(data[off:off+8], "\x00"))
		vsize := int(binary.LittleEndian.Uint32(data[off+8:]))
		rsize := int(binary.LittleEndian.Uint32(data[off+16:]))
		rptr := int(binary.LittleEndian.Uint32(data[off+20:]))
		size := vsize
		if rsize < size {
			size = rsize
		}
		if size < 0 || rptr < 0 {
			return nil, false, fmt.Errorf("section %s implausible", name)
		}
		var body []byte
		if rptr > 0 && size > 0 && rptr+size <= len(data) {
			body = data[rptr : rptr+size]
		}
		out[name] = body
	}
	return out, signed, nil
}

func walkInitrd(b []byte, d *imageData, prefixes, ignore []string, filterNoise bool) error {
	for len(b) > 0 {
		for len(b) > 0 && b[0] == 0 {
			b = b[1:]
		}
		if len(b) == 0 {
			return nil
		}
		if isGzip(b) {
			plain, err := gunzip(b)
			if err != nil {
				return err
			}
			_, err = scanCPIO(plain, d, prefixes, ignore, filterNoise)
			return err
		}
		if isNewc(b) {
			rest, err := scanCPIO(b, d, prefixes, ignore, filterNoise)
			if err != nil {
				return err
			}
			if len(rest) >= len(b) {
				return nil
			}
			b = rest
			continue
		}
		return nil
	}
	return nil
}

func isGzip(b []byte) bool { return len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b }
func isNewc(b []byte) bool {
	return len(b) >= 6 && (string(b[:6]) == "070701" || string(b[:6]) == "070702")
}

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Multistream(false)
	defer r.Close()
	return io.ReadAll(r)
}

// scanCPIO walks concatenated newc archives (later entries win). Returns
// unconsumed trailing bytes (gzip member, padding the caller didn't strip).
func scanCPIO(b []byte, d *imageData, prefixes, ignore []string, filterNoise bool) ([]byte, error) {
	i := 0
	saw := false
	for i+110 <= len(b) {
		for i < len(b) && b[i] == 0 {
			i++
		}
		if i+110 > len(b) {
			break
		}
		mag := string(b[i : i+6])
		if mag != "070701" && mag != "070702" {
			return b[i:], nil
		}
		filesize, err1 := hex8(b[i+54 : i+62])
		namesize, err2 := hex8(b[i+94 : i+102])
		mode, err3 := hex8(b[i+14 : i+22])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("cpio: bad header at %d", i)
		}
		if namesize < 1 || namesize > 4096 {
			return nil, fmt.Errorf("cpio: implausible namesize %d", namesize)
		}
		nameOff := i + 110
		if nameOff+namesize > len(b) {
			return nil, fmt.Errorf("cpio: name truncated")
		}
		name := string(b[nameOff : nameOff+namesize-1])
		hdrEnd := (110 + namesize + 3) &^ 3
		dataOff := i + hdrEnd
		if filesize < 0 || dataOff+filesize > len(b) {
			return nil, fmt.Errorf("cpio: data truncated for %s", name)
		}
		body := b[dataOff : dataOff+filesize]
		i = dataOff + ((filesize + 3) &^ 3)
		saw = true
		if name == "TRAILER!!!" {
			continue
		}
		name = path.Clean(strings.TrimPrefix(name, "/"))
		if name == "." || strings.HasPrefix(name, "../") {
			continue
		}
		recordCPIO(d, name, mode, body, prefixes, ignore, filterNoise)
	}
	if !saw {
		return b, nil
	}
	if i > len(b) {
		i = len(b)
	}
	return b[i:], nil
}

func hex8(b []byte) (int, error) {
	n, err := strconv.ParseUint(string(b), 16, 63)
	return int(n), err
}

func recordCPIO(d *imageData, name string, mode int, body []byte, prefixes, ignore []string, filterNoise bool) {
	typ := mode & 0xF000
	const ifReg, ifLnk = 0x8000, 0xA000
	if typ != ifReg && typ != ifLnk {
		return
	}
	if extra, ok := pkgTextParse(name, body); ok {
		for k, p := range extra {
			d.pkgs[k] = p
		}
	}
	tryRPM(d.pkgs, name, body)

	under := hasAnyPrefix(name, prefixes)
	tracked := under && !hasAnyPrefix(name, ignore)
	if tracked && filterNoise && isNoise(name) {
		tracked = false
	}
	filtered := under && !tracked
	if !tracked && !filtered {
		return
	}
	var id string
	if typ == ifLnk {
		id = "S:" + path.Clean(string(body))
	} else {
		sum := sha256.Sum256(body)
		id = "H:" + hex.EncodeToString(sum[:])
	}
	if tracked {
		d.files[name] = id
	} else {
		d.ignored[name] = id
	}
}

func pkgTextParse(name string, body []byte) (map[string]Pkg, bool) {
	switch name {
	case "lib/apk/db/installed", "usr/lib/apk/db/installed":
		return parseApk(body), true
	case "var/lib/dpkg/status":
		return parseDpkg(body), true
	}
	return nil, false
}

func tryRPM(pkgs map[string]Pkg, name string, body []byte) {
	base := path.Base(name)
	if (base != "rpmdb.sqlite" && base != "Packages" && base != "Packages.db") || !hasAnyPrefix(name, rpmDBDirs) || len(body) == 0 {
		return
	}
	f, err := os.CreateTemp("", "driftah-rpm-")
	if err != nil {
		return
	}
	tmp := f.Name()
	_, werr := f.Write(body)
	_ = f.Close()
	defer func() { _ = os.Remove(tmp) }()
	if werr != nil {
		return
	}
	extra, err := readRPM(tmp)
	if err != nil {
		return
	}
	for k, p := range extra {
		pkgs[k] = p
	}
}

func parseApk(b []byte) map[string]Pkg {
	out := map[string]Pkg{}
	for _, rec := range strings.Split(string(b), "\n\n") {
		p := Pkg{}
		for _, line := range strings.Split(rec, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch k {
			case "P":
				p.Name = v
			case "V":
				p.Version = v
			case "A":
				p.Arch = v
			}
		}
		if p.Name == "" || p.Version == "" {
			continue
		}
		out[p.nevraKey()] = p
	}
	return out
}

func parseDpkg(b []byte) map[string]Pkg {
	out := map[string]Pkg{}
	for _, rec := range strings.Split(string(b), "\n\n") {
		p := Pkg{}
		ok := false
		for _, line := range strings.Split(rec, "\n") {
			k, v, cut := strings.Cut(line, ": ")
			if !cut {
				continue
			}
			switch k {
			case "Package":
				p.Name = v
			case "Version":
				p.Version = v
			case "Architecture":
				p.Arch = v
			case "Status":
				ok = strings.Contains(v, "install ok installed")
			}
		}
		if !ok || p.Name == "" || p.Version == "" {
			continue
		}
		out[p.nevraKey()] = p
	}
	return out
}

func ukiDiff(from, to *ukiMeta) *UKIReport {
	if from == nil && to == nil {
		return nil
	}
	r := &UKIReport{Changes: []UKISectionChange{}}
	if to != nil {
		r.Arch, r.Signed, r.Cmdline, r.OSRel = to.Arch, to.Signed, to.Cmdline, to.OSRel
	} else if from != nil {
		r.Arch, r.Signed, r.Cmdline, r.OSRel = from.Arch, from.Signed, from.Cmdline, from.OSRel
	}
	names := map[string]bool{}
	var fs, ts map[string]ukiSec
	if from != nil {
		fs = from.Sections
		for n := range fs {
			names[n] = true
		}
	}
	if to != nil {
		ts = to.Sections
		for n := range ts {
			names[n] = true
		}
	}
	var list []string
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	for _, n := range list {
		a, b := fs[n], ts[n]
		if a.Display == b.Display && a.Size == b.Size {
			continue
		}
		r.Changes = append(r.Changes, UKISectionChange{
			Name: n, From: a.Display, To: b.Display, FromSize: a.Size, ToSize: b.Size,
		})
	}
	if from != nil && to != nil {
		if from.Arch != to.Arch {
			r.Changes = append(r.Changes, UKISectionChange{Name: "arch", From: from.Arch, To: to.Arch})
		}
		if from.Signed != to.Signed {
			r.Changes = append(r.Changes, UKISectionChange{
				Name: "signed",
				From: strconv.FormatBool(from.Signed),
				To:   strconv.FormatBool(to.Signed),
			})
		}
	}
	return r
}

func (u *UKIReport) empty() bool {
	return u == nil || len(u.Changes) == 0
}
