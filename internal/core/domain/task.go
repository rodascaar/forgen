package domain

import (
	"encoding/json"
	"time"
)

type TaskType string

const (
	TaskTypeExplore  TaskType = "explore"
	TaskTypePlan     TaskType = "plan"
	TaskTypeBuild    TaskType = "build"
	TaskTypeReview   TaskType = "review"
	TaskTypeResearch TaskType = "research"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type SubAgentConfig struct {
	Type        TaskType `json:"type"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Prompt      string   `json:"prompt"`
	MaxTurns    int      `json:"max_turns"`
	Timeout     int      `json:"timeout"`
}

type Task struct {
	ID          string         `json:"id"`
	Type        TaskType       `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Status      TaskStatus     `json:"status"`
	Result      *TaskResult    `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	ParentID    *string        `json:"parent_id,omitempty"`
	SubTasks    []*Task        `json:"sub_tasks,omitempty"`
	Config      SubAgentConfig `json:"config"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func NewTask(taskType TaskType, name, description string, config SubAgentConfig) *Task {
	return &Task{
		ID: generateID(), Type: taskType, Name: name, Description: description,
		Status: TaskStatusPending, CreatedAt: time.Now(), Config: config, Metadata: make(map[string]any),
	}
}
func (t *Task) MarkRunning()   { t.Status = TaskStatusRunning; n := time.Now(); t.StartedAt = &n }
func (t *Task) MarkCompleted(r *TaskResult) { t.Status = TaskStatusCompleted; n := time.Now(); t.CompletedAt = &n; t.Result = r }
func (t *Task) MarkFailed(e string) { t.Status = TaskStatusFailed; n := time.Now(); t.CompletedAt = &n; t.Error = e }
func (t *Task) MarkCancelled() { t.Status = TaskStatusCancelled; n := time.Now(); t.CompletedAt = &n }
func (t *Task) AddSubTask(s *Task) { s.ParentID = &t.ID; t.SubTasks = append(t.SubTasks, s) }
func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusCancelled
}
func (t *Task) Duration() time.Duration {
	if t.StartedAt == nil {
		return 0
	}
	end := time.Now()
	if t.CompletedAt != nil {
		end = *t.CompletedAt
	}
	return end.Sub(*t.StartedAt)
}

type TaskResult struct {
	Summary      string         `json:"summary"`
	FilesChanged []string       `json:"files_changed"`
	Output       string         `json:"output"`
	Artifacts    map[string]any `json:"artifacts"`
	Metrics      map[string]any `json:"metrics"`
}

type SubAgentRegistry struct{ Agents map[TaskType]SubAgentConfig `json:"agents"` }

func DefaultSubAgentRegistry() *SubAgentRegistry {
	return &SubAgentRegistry{Agents: map[TaskType]SubAgentConfig{
		TaskTypeExplore: {Type: TaskTypeExplore, Name: "Explorador", Description: "Explora el código base", Tools: []string{"read", "glob", "grep", "ls"}, Prompt: "Eres un explorador de código. No modifiques código.", MaxTurns: 20, Timeout: 120},
		TaskTypePlan: {Type: TaskTypePlan, Name: "Planificador", Description: "Diseña soluciones", Tools: []string{"read", "glob", "grep", "ls", "write"}, Prompt: "Eres un planificador técnico.", MaxTurns: 30, Timeout: 180},
		TaskTypeBuild: {Type: TaskTypeBuild, Name: "Constructor", Description: "Implementa código", Tools: []string{"read", "write", "edit", "bash", "glob", "grep", "ls"}, Prompt: "Eres un ingeniero que implementa código limpio.", MaxTurns: 50, Timeout: 300},
		TaskTypeReview: {Type: TaskTypeReview, Name: "Revisor", Description: "Revisa código", Tools: []string{"read", "glob", "grep", "ls", "bash"}, Prompt: "Eres un revisor experto.", MaxTurns: 20, Timeout: 120},
		TaskTypeResearch: {Type: TaskTypeResearch, Name: "Investigador", Description: "Investiga temas técnicos", Tools: []string{"read", "glob", "grep", "ls", "web_fetch", "web_search"}, Prompt: "Eres un investigador técnico.", MaxTurns: 30, Timeout: 180},
	}}
}
func (r *SubAgentRegistry) GetAgentConfig(t TaskType) (SubAgentConfig, bool) { c, ok := r.Agents[t]; return c, ok }

func (t Task) MarshalJSON() ([]byte, error) {
	type Alias Task
	return json.Marshal(struct {
		Alias
		Status string `json:"status"`
		Type   string `json:"type"`
	}{Alias: Alias(t), Status: string(t.Status), Type: string(t.Type)})
}
func (t *Task) UnmarshalJSON(data []byte) error {
	type Alias Task
	aux := struct {
		Alias
		Status string `json:"status"`
		Type   string `json:"type"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*t = Task(aux.Alias); t.Status = TaskStatus(aux.Status); t.Type = TaskType(aux.Type)
	return nil
}
