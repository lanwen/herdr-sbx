package guestplugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func archiveFixture(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	size := int64(3)
	if kind == tar.TypeSymlink {
		size = 0
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: size, Typeflag: kind, Linkname: "/tmp/other"}); err != nil {
		t.Fatal(err)
	}
	if size > 0 {
		tw.Write([]byte("bin"))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func TestDownloadVerifiesRelease(t *testing.T) {
	archive := archiveFixture(t, "herdr-reviewr", tar.TypeReg)
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "checksum mismatch"}[valid], func(t *testing.T) {
			transport := transportFunc(func(req *http.Request) (*http.Response, error) {
				if !strings.HasPrefix(req.URL.String(), "https://github.com/persiyanov/herdr-reviewr/releases/download/v0.36.2/herdr-reviewr-aarch64-unknown-linux-musl.") {
					t.Fatal(req.URL)
				}
				data := archive
				if strings.HasSuffix(req.URL.Path, ".sha256") {
					checksum := digest(archive)
					if !valid {
						checksum = strings.Repeat("0", 64)
					}
					data = []byte(checksum + "  asset.tar.gz\n")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
			})
			binary, err := download(context.Background(), transport, "0.36.2", "aarch64-unknown-linux-musl")
			if valid {
				if err != nil || string(binary) != "bin" {
					t.Fatalf("%q %v", binary, err)
				}
			} else if err == nil {
				t.Fatal("accepted bad checksum")
			}
		})
	}
}
func TestArchiveRejectsUnexpectedFiles(t *testing.T) {
	for _, name := range []string{"../herdr-reviewr", "/herdr-reviewr", "bin/other"} {
		if _, err := extractBinary(archiveFixture(t, name, tar.TypeReg)); err == nil {
			t.Fatal(name)
		}
	}
	if _, err := extractBinary(archiveFixture(t, "herdr-reviewr", tar.TypeSymlink)); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestPublishedReviewrRelease(t *testing.T) {
	if os.Getenv("SBX_TEST_REVIEWR_DOWNLOAD") != "1" {
		t.Skip("opt-in host release download")
	}
	for _, arch := range []string{"aarch64", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			binary, err := download(context.Background(), http.DefaultTransport, "0.36.2", arch+"-unknown-linux-musl")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(binary, []byte{0x7f, 'E', 'L', 'F'}) {
				t.Fatal("not Linux ELF")
			}
		})
	}
}

func TestPublishedLastRelease(t *testing.T) {
	if os.Getenv("SBX_TEST_REVIEWR_DOWNLOAD") != "1" {
		t.Skip("opt-in host release download")
	}
	for _, arch := range []string{"aarch64", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			binary, err := downloadLast(context.Background(), http.DefaultTransport, "0.1.0", arch+"-unknown-linux-musl")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(binary, []byte{0x7f, 'E', 'L', 'F'}) {
				t.Fatal("not Linux ELF")
			}
		})
	}
}
