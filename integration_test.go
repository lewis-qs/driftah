//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	fedoraLatestImage  = "quay.io/fedora/fedora:latest"
	fedoraRawhideImage = "quay.io/fedora/fedora:rawhide"
	fedoraReleasesJSON = "https://fedoraproject.org/releases.json"
	fedoraRawhideRPMs  = "https://dl.fedoraproject.org/pub/fedora/linux/development/rawhide/Everything/x86_64/os/Packages/k/"
)

var ukiRPMName = regexp.MustCompile(`kernel-uki-virt-[0-9][^"'<> ]+\.x86_64\.rpm`)

func TestIntegrationFedora(t *testing.T) {
	testImagePair(t, fedoraLatestImage, fedoraRawhideImage, 50)
}

func TestIntegrationAlpine(t *testing.T) {
	testImagePair(t, "docker.io/library/alpine:latest", "docker.io/library/alpine:edge", 8)
}

func TestIntegrationDebian(t *testing.T) {
	testImagePair(t, "docker.io/library/debian:stable", "docker.io/library/debian:unstable", 50)
}

func testImagePair(t *testing.T, fromRef, toRef string, minPkgs int) {
	t.Helper()
	const plat = "linux/amd64"
	prefixes := []string{"etc/", "usr/"}

	from, err := readImage(fromRef, plat, prefixes, nil, true)
	if err != nil {
		t.Fatalf("read %s: %v", fromRef, err)
	}
	to, err := readImage(toRef, plat, prefixes, nil, true)
	if err != nil {
		t.Fatalf("read %s: %v", toRef, err)
	}

	if len(from.pkgs) < minPkgs || len(to.pkgs) < minPkgs {
		t.Fatalf("suspiciously few packages: %d -> %d", len(from.pkgs), len(to.pkgs))
	}
	d := diff(from.pkgs, to.pkgs)
	fd := fileDiff(from.files, to.files, to.owners, prefixes)
	if d.empty() && fd.empty() {
		t.Fatalf("expected package or file drift between %s and %s", fromRef, toRef)
	}
	if self := diff(to.pkgs, to.pkgs); !self.empty() {
		t.Fatalf("self package-diff not empty: %+v", self)
	}
	if self := fileDiff(to.files, to.files, to.owners, prefixes); !self.empty() {
		t.Fatalf("self file-diff not empty: %d groups", len(self.Groups))
	}
}

func TestIntegrationSignedUKI(t *testing.T) {
	needTools(t)
	ver := fedoraLatestRelease(t)
	latestURL := fedoraUKIRPM(t, []string{
		fmt.Sprintf("https://dl.fedoraproject.org/pub/fedora/linux/updates/%s/Everything/x86_64/Packages/k/", ver),
		fmt.Sprintf("https://dl.fedoraproject.org/pub/fedora/linux/releases/%s/Everything/x86_64/os/Packages/k/", ver),
	})
	rawhideURL := fedoraUKIRPM(t, []string{fedoraRawhideRPMs})
	t.Logf("latest  %s", latestURL)
	t.Logf("rawhide %s", rawhideURL)

	dir := t.TempDir()
	fromD, fromU := loadUKI(t, filepath.Join(dir, "latest"), latestURL)
	toD, toU := loadUKI(t, filepath.Join(dir, "rawhide"), rawhideURL)

	for label, u := range map[string]*ukiMeta{"latest": fromU, "rawhide": toU} {
		if !u.Signed {
			t.Fatalf("%s UKI not Authenticode-signed", label)
		}
		if u.Arch != "x86_64" {
			t.Fatalf("%s arch = %s", label, u.Arch)
		}
		if !strings.Contains(u.OSRel, "ID=fedora") {
			t.Fatalf("%s osrel = %q", label, u.OSRel)
		}
		if !strings.Contains(u.Cmdline, "console=") {
			t.Fatalf("%s cmdline = %q", label, u.Cmdline)
		}
		if u.Sections[".linux"].Size < 1e6 || u.Sections[".initrd"].Size < 1e6 {
			t.Fatalf("%s section sizes linux=%d initrd=%d", label, u.Sections[".linux"].Size, u.Sections[".initrd"].Size)
		}
	}
	if u := ukiDiff(fromU, fromU); !u.empty() {
		t.Fatalf("self UKI diff not empty: %+v", u.Changes)
	}
	if u := ukiDiff(fromU, toU); u.empty() {
		t.Fatal("expected UKI section drift between latest and rawhide")
	}
	_ = fromD
	_ = toD
}

func needTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"rpm2cpio", "cpio"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
}

func fedoraLatestRelease(t *testing.T) string {
	t.Helper()
	body := httpGet(t, fedoraReleasesJSON)
	var rels []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &rels); err != nil {
		t.Fatal(err)
	}
	best := 0
	for _, r := range rels {
		n, err := strconv.Atoi(r.Version)
		if err == nil && n > best {
			best = n
		}
	}
	if best == 0 {
		t.Fatal("no numeric fedora release in releases.json")
	}
	return strconv.Itoa(best)
}

func fedoraUKIRPM(t *testing.T, listings []string) string {
	t.Helper()
	var lastErr string
	for _, listing := range listings {
		base, body, err := httpGetBase(t, listing)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		var hits []string
		for _, m := range ukiRPMName.FindAllString(string(body), -1) {
			if !strings.Contains(m, "addons") {
				hits = append(hits, m)
			}
		}
		if len(hits) == 0 {
			lastErr = "no kernel-uki-virt rpm in " + listing
			continue
		}
		sort.Strings(hits)
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		return base + hits[len(hits)-1]
	}
	t.Fatalf("kernel-uki-virt rpm: %s", lastErr)
	return ""
}

func loadUKI(t *testing.T, dir, rpmURL string) (*imageData, *ukiMeta) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rpm := filepath.Join(dir, "uki.rpm")
	download(t, rpmURL, rpm)
	efi := extractEFI(t, dir, rpm)
	d, meta, err := readUKI(efi, []string{"etc/", "usr/"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return d, meta
}

func httpGet(t *testing.T, url string) []byte {
	t.Helper()
	_, body, err := httpGetBase(t, url)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func httpGetBase(t *testing.T, url string) (string, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Request.URL.String(), body, nil
}

func download(t *testing.T, url, dest string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > maxUKIBytes {
		t.Fatalf("rpm too large: %d", resp.ContentLength)
	}
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxUKIBytes)); err != nil {
		t.Fatal(err)
	}
}

func extractEFI(t *testing.T, dir, rpm string) string {
	t.Helper()
	r2 := exec.Command("rpm2cpio", rpm)
	stdout, err := r2.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cp := exec.Command("cpio", "-id", "--quiet")
	cp.Dir = dir
	cp.Stdin = stdout
	if err := r2.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cp.Run(); err != nil {
		t.Fatalf("cpio: %v", err)
	}
	if err := r2.Wait(); err != nil {
		t.Fatalf("rpm2cpio: %v", err)
	}
	var efi string
	var size int64
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".efi") {
			return nil
		}
		if info.Size() > size {
			efi, size = p, info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if efi == "" {
		t.Fatal("no .efi in rpm")
	}
	return efi
}
