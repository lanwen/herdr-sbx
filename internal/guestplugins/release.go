package guestplugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func platformTarget(platform string) (string, error) {
	switch platform {
	case "Linux aarch64", "Linux arm64":
		return "aarch64-unknown-linux-musl", nil
	case "Linux x86_64":
		return "x86_64-unknown-linux-musl", nil
	}
	return "", fmt.Errorf("unsupported guest platform %q", platform)
}
func releaseClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many release redirects")
		}
		if req.URL.Scheme != "https" || (req.URL.Host != "github.com" && req.URL.Host != "release-assets.githubusercontent.com") {
			return errors.New("untrusted release redirect")
		}
		return nil
	}}
}
func fetch(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("plugin host download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin host download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("plugin download exceeds size limit")
	}
	return data, nil
}
func download(ctx context.Context, transport http.RoundTripper, version, target string) ([]byte, error) {
	base := "https://github.com/persiyanov/herdr-reviewr/releases/download/v" + version + "/herdr-reviewr-" + target
	client := releaseClient(transport)
	checksum, err := fetch(ctx, client, base+".sha256", 4096)
	if err != nil {
		return nil, err
	}
	archive, err := fetch(ctx, client, base+".tar.gz", 128<<20)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(checksum))
	if len(fields) == 0 || fields[0] != digest(archive) {
		return nil, errors.New("Reviewr release checksum mismatch")
	}
	return extractBinary(archive)
}
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var binary []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Name != "herdr-reviewr" && h.Name != "./herdr-reviewr" {
			return nil, errors.New("unexpected file in Reviewr archive")
		}
		if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 128<<20 || binary != nil {
			return nil, errors.New("invalid Reviewr binary archive")
		}
		binary, err = io.ReadAll(io.LimitReader(tr, (128<<20)+1))
		if err != nil {
			return nil, err
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("Reviewr archive has no binary")
	}
	return binary, nil
}

func downloadLast(ctx context.Context, transport http.RoundTripper, version, target string) ([]byte, error) {
	arch := "amd64"
	if strings.HasPrefix(target, "aarch64") {
		arch = "arm64"
	}
	asset := "herdr-last-linux-" + arch
	base := "https://github.com/lmilojevicc/herdr-last/releases/download/v" + version + "/"
	client := releaseClient(transport)
	sums, err := fetch(ctx, client, base+"SHA256SUMS", 64<<10)
	if err != nil {
		return nil, err
	}
	binary, err := fetch(ctx, client, base+asset, 128<<20)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(sums), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && strings.TrimPrefix(parts[1], "*") == asset {
			if parts[0] != digest(binary) {
				return nil, errors.New("herdr-last release checksum mismatch")
			}
			return binary, nil
		}
	}
	return nil, errors.New("herdr-last release checksum is missing")
}
