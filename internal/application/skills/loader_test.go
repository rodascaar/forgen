package skills_test

import (
	"context"
	"testing"

	"github.com/forgen/forgen/internal/adapters/out/fs"
	"github.com/forgen/forgen/internal/application/skills"
)

func setupSkills(t *testing.T) (*fs.OSFileSystem, []string) {
	t.Helper()
	dir := t.TempDir()
	fileSystem := fs.New(dir)

	_ = fileSystem.Write(context.Background(), "skills/go-lint/SKILL.md", []byte(`---
name: go-lint
description: Configura golangci-lint para un proyecto Go
---
Añade un .golangci.yml y corre golangci-lint run ./...
`))
	_ = fileSystem.Write(context.Background(), "skills/deploy/SKILL.md", []byte(`---
name: deploy
description: Pasos para desplegar a producción
---
Ejecuta make build y sube el binario.
`))
	return fileSystem, []string{"skills"}
}

func TestDiscoverParsesFrontmatter(t *testing.T) {
	fileSystem, dirs := setupSkills(t)
	discovered, err := skills.Discover(context.Background(), dirs, fileSystem)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("skills = %d, want 2", len(discovered))
	}
	names := map[string]bool{}
	for _, skill := range discovered {
		names[skill.Name] = true
	}
	if !names["go-lint"] || !names["deploy"] {
		t.Fatalf("nombres = %v", names)
	}
}

func TestCatalogListsSkills(t *testing.T) {
	fileSystem, dirs := setupSkills(t)
	discovered, _ := skills.Discover(context.Background(), dirs, fileSystem)
	catalog := skills.Catalog(discovered)
	if catalog == "" {
		t.Fatal("catalog vacío")
	}
}

func TestResolveSkill(t *testing.T) {
	fileSystem, dirs := setupSkills(t)
	discovered, _ := skills.Discover(context.Background(), dirs, fileSystem)
	skill, ok := skills.ResolveSkill(discovered, "go-lint")
	if !ok {
		t.Fatal("go-lint no resuelta")
	}
	if skill.Body == "" {
		t.Fatal("cuerpo vacío")
	}
	if _, ok := skills.ResolveSkill(discovered, "no-existe"); ok {
		t.Fatal("skill inexistente no debería resolverse")
	}
}

func TestParseSkillWithFrontmatter(t *testing.T) {
	content := `---
name: my-skill
description: hace algo
---
instrucciones detalladas`
	skill := skills.ParseSkill(content, "skills/my-skill/SKILL.md")
	if skill.Name != "my-skill" {
		t.Fatalf("name = %q", skill.Name)
	}
	if skill.Description != "hace algo" {
		t.Fatalf("description = %q", skill.Description)
	}
	if skill.Body != "instrucciones detalladas" {
		t.Fatalf("body = %q", skill.Body)
	}
}

func TestParseSkillWithoutFrontmatter(t *testing.T) {
	content := "# Uso\nExplica cómo usar la herramienta."
	skill := skills.ParseSkill(content, "skills/useful/SKILL.md")
	if skill.Name != "useful" {
		t.Fatalf("name = %q, want useful (del directorio)", skill.Name)
	}
	if skill.Description == "" {
		t.Fatal("descripción inferida vacía")
	}
}
