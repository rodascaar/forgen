package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.1", -1},
		{"v0.1.2", "0.1.2", 0},
		{"0.1.3", "0.1.2", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.2.0", "0.10.0", -1},
		{"0.1.2-rc1", "0.1.2", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q)=%d, quiero %d", c.a, c.b, got, c.want)
		}
	}
}

// makeArchive crea un forgen_{os}_{arch}.tar.gz con un binario "forgen" falso
// y devuelve su checksum SHA-256.
func makeArchive(t *testing.T) (string, []byte) {
	t.Helper()
	asset := fmt.Sprintf("forgen_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	var buf []byte
	gzBuf := &bytesBuffer{}
	gz := gzip.NewWriter(gzBuf)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\necho fake-forgen\n")
	hdr := &tar.Header{Name: "forgen", Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	buf = gzBuf.Bytes()
	return asset, buf
}

type bytesBuffer struct{ b []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) { b.b = append(b.b, p...); return len(p), nil }
func (b *bytesBuffer) Bytes() []byte               { return b.b }

func TestServiceCheckAndApply(t *testing.T) {
	asset, archive := makeArchive(t)
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case "/download/v9.9.9/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		case "/download/v9.9.9/" + asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := New()
	svc.APIURL = server.URL + "/latest"
	svc.DownloadBase = server.URL + "/download"
	svc.CurrentVersion = func() string { return "0.1.2" }

	release, hasUpdate, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release.Tag != "v9.9.9" || !hasUpdate {
		t.Fatalf("Check inesperado: %+v hasUpdate=%v", release, hasUpdate)
	}

	// Sin update cuando la versión local es más nueva.
	svc.CurrentVersion = func() string { return "10.0.0" }
	_, hasUpdate, err = svc.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hasUpdate {
		t.Fatal("no debería haber update con versión local mayor")
	}

	// Apply: instalar en un directorio temporal apuntando Executable.
	binDir := t.TempDir()
	svc.Executable = func() (string, error) { return filepath.Join(binDir, "forgen"), nil }

	svc.CurrentVersion = func() string { return "0.1.2" }
	if err := svc.Apply(context.Background(), "v9.9.9"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(binDir, "forgen"))
	if err != nil {
		t.Fatalf("binario no instalado: %v", err)
	}
	if string(installed) != "#!/bin/sh\necho fake-forgen\n" {
		t.Fatalf("contenido inesperado: %q", installed)
	}
}

func TestServiceApplyChecksumMismatch(t *testing.T) {
	asset, _ := makeArchive(t)
	badChecksums := "0000000000000000000000000000000000000000000000000000000000000000  " + asset + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v1.0.0/checksums.txt":
			_, _ = w.Write([]byte(badChecksums))
		case "/download/v1.0.0/" + asset:
			_, _ = w.Write([]byte("datos"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := New()
	svc.DownloadBase = server.URL + "/download"
	if err := svc.Apply(context.Background(), "v1.0.0"); err == nil {
		t.Fatal("se esperaba un error por checksum no coincidente")
	}
}
