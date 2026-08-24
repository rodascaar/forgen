package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newDoctorCommand construye el diagnóstico de entorno.
func newDoctorCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnostica el entorno: config, keys, binarios y workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), app)
		},
	}
}

func runDoctor(ctx context.Context, app *apppkg.App) error {
	report := &diagnosticReport{}
	report.line("forgen doctor")

	// Configuración.
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		report.fail("configuración inválida: %v", err)
	} else {
		report.ok("configuración en %s", app.Paths.ConfigFile)
		report.line("  agente por defecto: %s", config.Agent)
		report.line("  proveedor/modelo: %s/%s", config.Default.Provider, config.Default.Model)
		report.line("  modo de permisos: %s", config.Permissions.Mode)
		for _, provider := range config.Providers {
			report.checkProvider(provider)
		}
	}

	// MCP servers.
	if len(config.MCPServers) > 0 {
		report.ok("mcp servers: %d configurados", len(config.MCPServers))
		for name, s := range config.MCPServers {
			target := s.Command
			if s.URL != "" {
				target = s.URL
			}
			report.line("  mcp %s (%s) → %s", name, s.MCPServerType(), target)
		}
	} else {
		report.line("mcp: sin servidores configurados")
	}

	// LSP
	if app.LSP != nil {
		report.ok("lsp: activo")
	} else {
		report.line("lsp: no detectado (instala gopls / typescript-language-server)")
	}
	// Hooks
	hookDirs := []string{app.Paths.ConfigDir + "/hooks/bash", ".forgen/hooks/bash"}
	for _, dir := range hookDirs {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			report.ok("hooks bash: %d en %s", len(entries), dir)
		}
	}
	if app.ToolRegistry != nil {
		report.line("tools registradas: %d", len(app.ToolRegistry.ListTools()))
	}

	// Binarios necesarios.
	for _, binary := range []string{"git"} {
		if isAvailable(binary) {
			report.ok("binario %s disponible", binary)
		} else {
			report.warn("binario %s NO encontrado (requerido)", binary)
		}
	}

	// Workspace.
	workspace, _ := os.Getwd()
	report.line("workspace: %s", workspace)
	if language, detectErr := app.Language.Detect(ctx, workspace); detectErr == nil && language != "" {
		report.ok("lenguaje detectado: %s", language)
	} else {
		report.warn("lenguaje no detectado")
	}
	if toolchain, probeErr := app.Toolchain.Probe(ctx, workspace); probeErr == nil && toolchain != "" {
		report.ok("%s", toolchain)
	}

	fmt.Print(report.String())
	return nil
}

type diagnosticReport struct {
	lines []string
}

func (r *diagnosticReport) line(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *diagnosticReport) ok(format string, args ...any) {
	r.lines = append(r.lines, "✓ "+fmt.Sprintf(format, args...))
}

func (r *diagnosticReport) warn(format string, args ...any) {
	r.lines = append(r.lines, "⚠ "+fmt.Sprintf(format, args...))
}

func (r *diagnosticReport) fail(format string, args ...any) {
	r.lines = append(r.lines, "✗ "+fmt.Sprintf(format, args...))
}

func (r *diagnosticReport) checkProvider(provider domain.ProviderConfig) {
	keySet := provider.APIKeyEnv == "" || os.Getenv(provider.APIKeyEnv) != ""
	if keySet {
		r.ok("proveedor %s (%s) con key configurada", provider.Name, provider.Type)
	} else {
		r.warn("proveedor %s (%s): la variable %q no está definida",
			provider.Name, provider.Type, provider.APIKeyEnv)
	}
}

func (r *diagnosticReport) String() string {
	result := ""
	for _, line := range r.lines {
		result += line + "\n"
	}
	return result
}

func isAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
