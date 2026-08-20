package domain

// ModelRole es el papel de un modelo en la orquestación multi-modelo.
type ModelRole string

const (
	RoleOrchestrator ModelRole = "orchestrator"
	RolePlanner      ModelRole = "planner"
	RoleBuilder      ModelRole = "builder"
	RoleReviewer     ModelRole = "reviewer"
	RoleExplorer     ModelRole = "explorer"
	RoleResearcher   ModelRole = "researcher"
)

// AgentPhase es la fase de trabajo actual del agente (tracking y atribución).
type AgentPhase string

const (
	PhaseExplore  AgentPhase = "explore"
	PhasePlan     AgentPhase = "plan"
	PhaseBuild    AgentPhase = "build"
	PhaseReview   AgentPhase = "review"
	PhaseResearch AgentPhase = "research"
)

// Tier es el nivel de un modelo para el routing por complejidad.
type Tier string

const (
	TierLight    Tier = "light"
	TierStandard Tier = "standard"
	TierHeavy    Tier = "heavy"
)

// DefaultRoles devuelve el orden canónico de los roles.
func DefaultRoles() []ModelRole {
	return []ModelRole{RoleOrchestrator, RolePlanner, RoleBuilder, RoleReviewer, RoleExplorer, RoleResearcher}
}

// ModelMetadata describe capacidades de un modelo para el routing.
type ModelMetadata struct {
	Tier   Tier   `yaml:"tier"`
	Vision bool   `yaml:"vision"`
	Desc   string `yaml:"description,omitempty"`
}
