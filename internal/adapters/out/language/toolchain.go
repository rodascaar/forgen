package language

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// ToolchainProbe detecta manifiestos y gestores de paquetes de un proyecto.
type ToolchainProbe struct{}

// NewToolchainProbe construye el probe de toolchain.
func NewToolchainProbe() *ToolchainProbe { return &ToolchainProbe{} }

// manifestHints mapea manifiestos a descripciones legibles.
var manifestHints = []struct {
	file  string
	label string
}{
	{"go.mod", "Go module"},
	{"package.json", "Node.js (JS/TS)"},
	{"pnpm-lock.yaml", "  gestor: pnpm"},
	{"yarn.lock", "  gestor: yarn"},
	{"package-lock.json", "  gestor: npm"},
	{"bun.lockb", "  gestor: bun"},
	{"pyproject.toml", "Python (pyproject)"},
	{"requirements.txt", "Python (requirements)"},
	{"Pipfile", "Python (Pipenv)"},
	{"Cargo.toml", "Rust (Cargo)"},
	{"Gemfile", "Ruby (Bundler)"},
	{"composer.json", "PHP (Composer)"},
	{"pom.xml", "Java (Maven)"},
	{"build.gradle", "Java (Gradle)"},
	{"build.gradle.kts", "Kotlin (Gradle)"},
	{"*.csproj", ".NET/C#"},
	{"Makefile", "Make"},
	{"Dockerfile", "Docker"},
	{"docker-compose.yml", "Docker Compose"},
	{"docker-compose.yaml", "Docker Compose"},
	{"terraform/*.tf", "Terraform"},
	{"mix.exs", "Elixir (Mix)"},
	{"go.work", "Go workspace"},
}

// conventionCommands mapea manifiestos a los comandos estándar del ecosistema.
var conventionCommands = []struct {
	manifests []string
	commands  []string
}{
	{[]string{"go.mod", "go.work"}, []string{
		"test: go test ./...",
		"build: go build ./...",
		"lint: go vet ./...",
	}},
	{[]string{"package.json"}, []string{
		"test: npm test",
		"build: npm run build",
		"lint: npm run lint",
	}},
	{[]string{"pyproject.toml", "requirements.txt", "Pipfile"}, []string{
		"test: pytest",
		"lint: ruff check .",
	}},
	{[]string{"Cargo.toml"}, []string{
		"test: cargo test",
		"build: cargo build",
		"lint: cargo clippy",
	}},
	{[]string{"Gemfile"}, []string{"test: bundle exec rspec"}},
	{[]string{"mix.exs"}, []string{"test: mix test"}},
	{[]string{"composer.json"}, []string{"test: composer test"}},
}

// Probe implementa ports.ToolchainProbe.
func (t *ToolchainProbe) Probe(_ context.Context, dir string) (string, error) {
	var lines []string
	detected := map[string]bool{}

	for _, hint := range manifestHints {
		if found, _ := findManifest(dir, hint.file); found {
			lines = append(lines, hint.label)
		}
	}

	// Convenciones de comandos por ecosistema.
	for _, convention := range conventionCommands {
		for _, manifest := range convention.manifests {
			if found, _ := findManifest(dir, manifest); found {
				for _, command := range convention.commands {
					if !detected[command] {
						detected[command] = true
						lines = append(lines, "  "+command)
					}
				}
				break
			}
		}
	}

	if len(lines) == 0 {
		return "", nil
	}
	return "Toolchain detectado: " + strings.Join(lines, "\n  - "), nil
}

func findManifest(dir, name string) (bool, error) {
	if strings.HasPrefix(name, "*.") {
		// Patrón de extensión: buscar un archivo con esa extensión en la raíz.
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		extension := strings.TrimPrefix(name, "*")
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), extension) {
				return true, nil
			}
		}
		return false, nil
	}
	if strings.Contains(name, "/") {
		// Ruta anidada: probar bajo el directorio.
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil, nil
	}
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil, nil
}

// Summary retorna la descripción compacta para el system prompt.
func (t *ToolchainProbe) Summary(dir string) string {
	text, err := t.Probe(context.Background(), dir)
	if err != nil {
		return fmt.Sprintf("Error detectando toolchain: %v", err)
	}
	return text
}

var _ ports.ToolchainProbe = (*ToolchainProbe)(nil)
