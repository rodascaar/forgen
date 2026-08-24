// Package upgrade implementa la auto-actualización de forgen desde GitHub
// Releases, reutilizando el mismo esquema de assets que scripts/install.sh
// (forgen_{os}_{arch}.tar.gz + checksums.txt).
package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultRepo = "rodascaar/forgen"

// ReleaseInfo describe la última release publicada.
type ReleaseInfo struct {
	Tag string `json:"tag_name"`
}

// Service comprueba y aplica actualizaciones.
type Service struct {
	HTTP           *http.Client
	Repo           string
	APIURL         string // p.ej. https://api.github.com/repos/<repo>/releases/latest
	DownloadBase   string // p.ej. https://github.com/<repo>/releases/download
	CurrentVersion func() string
	// Executable devuelve la ruta del binario actual (inyectable para tests).
	Executable func() (string, error)
}

// New construye el servicio con los valores por defecto de GitHub.
func New() *Service {
	return &Service{
		HTTP:           &http.Client{Timeout: 30 * time.Second},
		Repo:           defaultRepo,
		APIURL:         "https://api.github.com/repos/" + defaultRepo + "/releases/latest",
		DownloadBase:   "https://github.com/" + defaultRepo + "/releases/download",
		CurrentVersion: func() string { return "0.1.0" },
		Executable:     os.Executable,
	}
}

// Check consulta la última release y devuelve si hay una versión más nueva.
// Nunca modifica el sistema; usa --check en el CLI para solo informar.
func (s *Service) Check(ctx context.Context) (ReleaseInfo, bool, error) {
	release, err := s.latestRelease(ctx)
	if err != nil {
		return ReleaseInfo{}, false, err
	}
	if s.CurrentVersion == nil {
		return release, true, nil
	}
	return release, CompareVersions(release.Tag, s.CurrentVersion()) > 0, nil
}

// Apply descarga la versión indicada, verifica su checksum y reemplaza el
// binario en ejecución (o cae a ~/.local/bin si no hay permisos).
func (s *Service) Apply(ctx context.Context, version string) error {
	asset := "forgen_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	base := s.DownloadBase + "/" + version

	// Descargar checksums y archivo.
	checksums, err := s.fetch(ctx, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("descargar checksums: %w", err)
	}
	archive, err := s.fetch(ctx, base+"/"+asset)
	if err != nil {
		return fmt.Errorf("descargar %s: %w", asset, err)
	}

	// Verificar SHA-256 del archivo contra checksums.txt.
	if want := checksumFor(checksums, asset); want != "" {
		sum := sha256.Sum256(archive)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
			return fmt.Errorf("checksum de %s no coincide", asset)
		}
	}

	// Extraer el binario a un directorio temporal.
	dir, err := os.MkdirTemp("", "forgen-upgrade-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	archivePath := filepath.Join(dir, asset)
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		return err
	}
	if err := extractTarGz(archivePath, dir); err != nil {
		return err
	}

	binData, err := os.ReadFile(filepath.Join(dir, "forgen"))
	if err != nil {
		return fmt.Errorf("extraer binario: %w", err)
	}

	// Instalar sobre el binario actual; fallback a ~/.local/bin.
	return s.install(binData)
}

// install escribe el binario en la ruta de ejecución o en ~/.local/bin.
func (s *Service) install(data []byte) error {
	targets := []string{}
	if s.Executable != nil {
		if exe, err := s.Executable(); err == nil && exe != "" {
			targets = append(targets, exe)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		targets = append(targets, filepath.Join(home, ".local", "bin", "forgen"))
	}

	var lastErr error
	for _, target := range targets {
		if target == "" {
			continue
		}
		if err := writeExecutable(target, data); err != nil {
			lastErr = err
			continue
		}
		fmt.Fprintf(os.Stderr, "forgen actualizado en %s\n", target)
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("no se pudo determinar dónde instalar el binario")
	}
	return fmt.Errorf("no se pudo reemplazar el binario: %w", lastErr)
}

// writeExecutable escribe data en target de forma atómica (temp + rename).
func writeExecutable(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := target + ".new"
	// #nosec G703 -- target es la ruta del binario en ejecución/instalación
	// (os.Executable o ~/.local/bin), no entrada de usuario.
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// latestRelease consulta la última release de GitHub.
func (s *Service) latestRelease(ctx context.Context) (ReleaseInfo, error) {
	body, err := s.fetch(ctx, s.APIURL)
	if err != nil {
		return ReleaseInfo{}, err
	}
	var release ReleaseInfo
	if err := json.Unmarshal(body, &release); err != nil {
		return ReleaseInfo{}, fmt.Errorf("parsear release: %w", err)
	}
	if release.Tag == "" {
		return ReleaseInfo{}, fmt.Errorf("no se encontró la última release")
	}
	return release, nil
}

// fetch descarga una URL con un User-Agent (GitHub API lo exige).
func (s *Service) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "forgen-upgrade")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d para %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// checksumFor extrae el hash SHA-256 del asset de checksums.txt.
func checksumFor(checksums []byte, asset string) string {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == asset {
			return fields[0]
		}
	}
	return ""
}

// extractTarGz extrae el contenido de un .tar.gz al directorio destino.
func extractTarGz(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// Evitar escritura fuera del destino (path traversal).
		name := filepath.Base(header.Name)
		out := filepath.Join(dest, name)
		if !strings.HasPrefix(out, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("ruta insegura en el archivo: %s", header.Name)
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		// Copia acotada para evitar un ataque de "decompression bomb".
		const maxDecompressed = 512 << 20 // 512 MiB
		written, err := io.Copy(f, io.LimitReader(tr, maxDecompressed+1))
		if err != nil {
			_ = f.Close()
			return err
		}
		if written > maxDecompressed {
			_ = f.Close()
			return fmt.Errorf("entrada del archivo demasiado grande")
		}
		_ = f.Close()
	}
	return nil
}

// CompareVersions compara dos versiones semver (ignorando el prefijo 'v' y
// cualquier sufijo pre-release). Devuelve 1 si a>b, -1 si a<b, 0 si iguales.
func CompareVersions(a, b string) int {
	as := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	bs := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(as) {
			ai, _ = strconv.Atoi(strings.TrimSpace(as[i]))
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(strings.TrimSpace(bs[i]))
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}
