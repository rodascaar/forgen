package domain

// PermissionLevel define cómo se autoriza una herramienta.
type PermissionLevel string

const (
	PermissionAuto      PermissionLevel = "auto"       // se ejecuta sin preguntar
	PermissionOnRequest PermissionLevel = "on_request" // se pregunta al usuario
	PermissionNever     PermissionLevel = "never"      // se rechaza siempre
)

// PermissionMode es el modo global de permisos de la sesión.
type PermissionMode string

const (
	PermissionModeAuto      PermissionMode = "auto"
	PermissionModeOnRequest PermissionMode = "on_request"
	PermissionModeNever     PermissionMode = "never"
)

// PermissionRule es una regla persistente de permiso para una herramienta.
type PermissionRule struct {
	Tool      string          `yaml:"tool"`
	Arguments map[string]any  `yaml:"arguments,omitempty"`
	Level     PermissionLevel `yaml:"level"`
	Workspace string          `yaml:"workspace,omitempty"`
	IsExact   bool            `yaml:"is_exact,omitempty"` // true = coincide solo si Arguments es exactamente igual
}

// Decision sobre un intento de ejecutar una herramienta.
type Decision struct {
	Allowed bool
	Level   PermissionLevel
	Reason  string
}

func (d Decision) Denied() bool { return !d.Allowed }
