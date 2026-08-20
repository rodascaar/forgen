// Package permission decide qué herramientas están autorizadas.
package permission

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// sensitiveTools requieren confirmación en modo on_request.
var sensitiveTools = map[string]bool{
	"write": true,
	"edit":  true,
	"bash":  true,
}

// dangerousPatterns se detectan incluso en modo auto (fail-safe).
var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bsudo\b`),
	regexp.MustCompile(`(?i)\brm\s+(-rf?|--recursive)\s+/`),
	regexp.MustCompile(`(?i)\b(dd|mkfs\.\w+|fdisk|shutdown|reboot)\b`),
	regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:&\s*\};:`),
	regexp.MustCompile(`(?i)\bchmod\s+777\b`),
}

// Service implementa ports.PermissionDecider.
type Service struct {
	mode      domain.PermissionMode
	workspace string
	rules     []domain.PermissionRule
}

// NewService construye el decisor con el modo global, las reglas de config
// y las reglas persistentes del usuario.
func NewService(mode domain.PermissionMode, workspace string, configRules, persistedRules []domain.PermissionRule) *Service {
	rules := make([]domain.PermissionRule, 0, len(configRules)+len(persistedRules))
	rules = append(rules, configRules...)
	rules = append(rules, persistedRules...)
	return &Service{mode: mode, workspace: workspace, rules: rules}
}

// Decide implementa ports.PermissionDecider.
func (s *Service) Decide(_ context.Context, sessionID string, call domain.ToolCall) (domain.Decision, error) {
	// 1. Reglas explícitas primero (mayor precedencia).
	if rule, ok := s.matchRule(call); ok {
		return decisionFromLevel(rule.Level, "regla de permiso aplicada"), nil
	}

	// 2. Detección de peligro: se pregunta siempre.
	if s.isDangerous(call) {
		return domain.Decision{Allowed: false, Level: domain.PermissionOnRequest,
			Reason: "comando potencialmente destructivo detectado"}, nil
	}

	// 3. Modo global.
	switch s.mode {
	case domain.PermissionModeNever:
		return domain.Decision{Allowed: false, Level: domain.PermissionNever,
			Reason: "modo de permisos 'never' (solo lectura)"}, nil
	case domain.PermissionModeOnRequest:
		if sensitiveTools[call.Name] {
			return domain.Decision{Allowed: false, Level: domain.PermissionOnRequest,
				Reason: "herramienta sensible en modo on_request"}, nil
		}
		return domain.Decision{Allowed: true, Level: domain.PermissionAuto,
			Reason: "herramienta de lectura segura"}, nil
	default: // auto
		return domain.Decision{Allowed: true, Level: domain.PermissionAuto,
			Reason: "modo auto"}, nil
	}
}

func decisionFromLevel(level domain.PermissionLevel, reason string) domain.Decision {
	switch level {
	case domain.PermissionAuto:
		return domain.Decision{Allowed: true, Level: domain.PermissionAuto, Reason: reason}
	case domain.PermissionNever:
		return domain.Decision{Allowed: false, Level: domain.PermissionNever, Reason: reason}
	default:
		return domain.Decision{Allowed: false, Level: domain.PermissionOnRequest, Reason: reason}
	}
}

func (s *Service) matchRule(call domain.ToolCall) (domain.PermissionRule, bool) {
	for _, rule := range s.rules {
		if rule.Tool != call.Name {
			continue
		}
		if rule.Workspace != "" && rule.Workspace != s.workspace {
			continue
		}
		if rule.IsExact {
			if argsEqual(rule.Arguments, call.Arguments) {
				return rule, true
			}
			continue
		}
		if argsSubset(rule.Arguments, call.Arguments) {
			return rule, true
		}
	}
	return domain.PermissionRule{}, false
}

func argsEqual(a, b map[string]any) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func argsSubset(rule, call map[string]any) bool {
	for key, ruleValue := range rule {
		callValue, ok := call[key]
		if !ok || fmt.Sprintf("%v", callValue) != fmt.Sprintf("%v", ruleValue) {
			return false
		}
	}
	return true
}

func (s *Service) isDangerous(call domain.ToolCall) bool {
	if call.Name != "bash" {
		return false
	}
	command, _ := call.Arguments["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

// RuleFor builds a rule to persist for a tool call.
func RuleFor(call domain.ToolCall, level domain.PermissionLevel, workspace string) domain.PermissionRule {
	return domain.PermissionRule{
		Tool:      call.Name,
		Arguments: call.Arguments,
		Level:     level,
		Workspace: workspace,
		IsExact:   true,
	}
}

var _ ports.PermissionDecider = (*Service)(nil)
