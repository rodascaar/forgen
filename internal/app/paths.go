package app

import (
	"os"
	"path/filepath"
)

// Paths agrupa las rutas del sistema de archivos de forgen (XDG).
type Paths struct {
	ConfigDir       string
	DataDir         string
	ConfigFile      string
	SessionsDir     string
	FermentsDir     string
	UsageFile       string
	RulesFile       string
	CredentialsFile string
	TodosFile       string
	TasksFile       string
}

// ResolvePaths calcula las rutas siguiendo la especificación XDG, con
// override explícito vía FORGEN_CONFIG_DIR / FORGEN_DATA_DIR.
func ResolvePaths() Paths {
	configDir := envOr("FORGEN_CONFIG_DIR", "")
	if configDir == "" {
		configDir = filepath.Join(xdgConfigHome(), "forgen")
	}
	dataDir := envOr("FORGEN_DATA_DIR", "")
	if dataDir == "" {
		dataDir = filepath.Join(xdgDataHome(), "forgen")
	}
	return Paths{
		ConfigDir:       configDir,
		DataDir:         dataDir,
		ConfigFile:      filepath.Join(configDir, "config.yaml"),
		SessionsDir:     filepath.Join(dataDir, "sessions"),
		FermentsDir:     filepath.Join(dataDir, "ferments"),
		UsageFile:       filepath.Join(dataDir, "usage.jsonl"),
		RulesFile:       filepath.Join(dataDir, "permissions.yaml"),
		CredentialsFile: filepath.Join(configDir, "credentials"),
		TodosFile:       filepath.Join(dataDir, "todos.jsonl"),
		TasksFile:       filepath.Join(dataDir, "tasks.jsonl"),
	}
}

func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.SessionsDir, p.FermentsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func xdgConfigHome() string {
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func xdgDataHome() string {
	if value := os.Getenv("XDG_DATA_HOME"); value != "" {
		return value
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
