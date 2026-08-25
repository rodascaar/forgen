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

// PermissionChoice es la respuesta del usuario a una confirmación de permiso.
// Remember=true persiste la regla como "permitir siempre" (nivel Auto).
type PermissionChoice struct {
	Allowed bool
	Remember bool
}

// ChoiceDeny niega una vez.
func ChoiceDeny() PermissionChoice { return PermissionChoice{Allowed: false} }

// ChoiceAllow permite una vez.
func ChoiceAllow() PermissionChoice { return PermissionChoice{Allowed: true} }

// ChoiceAllowAlways permite y persiste la regla.
func ChoiceAllowAlways() PermissionChoice { return PermissionChoice{Allowed: true, Remember: true} }
