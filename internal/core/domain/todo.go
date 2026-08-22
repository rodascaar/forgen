package domain

import (
	"encoding/json"
	"time"
)

// TodoStatus representa el estado de una tarea.
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusDone       TodoStatus = "done"
	TodoStatusCancelled  TodoStatus = "cancelled"
)

// Todo representa una tarea individual en la lista de tareas.
type Todo struct {
	ID          string     `json:"id"`
	Content     string     `json:"content"`
	Status      TodoStatus `json:"status"`
	ActiveForm  string     `json:"active_form"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	ParentID    *string    `json:"parent_id,omitempty"`
	Order       int        `json:"order"`
}

func NewTodo(content, activeForm string) *Todo {
	now := time.Now()
	return &Todo{
		ID:         generateID(),
		Content:    content,
		ActiveForm: activeForm,
		Status:     TodoStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (t *Todo) MarkInProgress() { t.Status = TodoStatusInProgress; t.UpdatedAt = time.Now() }
func (t *Todo) MarkDone() {
	t.Status = TodoStatusDone; t.UpdatedAt = time.Now()
	now := time.Now(); t.CompletedAt = &now
}
func (t *Todo) MarkCancelled() { t.Status = TodoStatusCancelled; t.UpdatedAt = time.Now() }
func (t *Todo) MarkPending()   { t.Status = TodoStatusPending; t.UpdatedAt = time.Now(); t.CompletedAt = nil }
func (t *Todo) UpdateContent(c, a string) { t.Content = c; t.ActiveForm = a; t.UpdatedAt = time.Now() }
func (t *Todo) SetOrder(o int) { t.Order = o; t.UpdatedAt = time.Now() }
func (t *Todo) IsDone() bool   { return t.Status == TodoStatusDone }
func (t *Todo) IsActive() bool { return t.Status == TodoStatusPending || t.Status == TodoStatusInProgress }

func (t Todo) MarshalJSON() ([]byte, error) {
	type Alias Todo
	return json.Marshal(struct {
		Alias
		Status string `json:"status"`
	}{Alias: Alias(t), Status: string(t.Status)})
}
func (t *Todo) UnmarshalJSON(data []byte) error {
	type Alias Todo
	aux := struct {
		Alias
		Status string `json:"status"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*t = Todo(aux.Alias); t.Status = TodoStatus(aux.Status)
	return nil
}

// TodoList representa una lista de tareas con metadatos.
type TodoList struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Todos     []*Todo   `json:"todos"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewTodoList(name string) *TodoList {
	now := time.Now()
	return &TodoList{ID: generateID(), Name: name, Todos: make([]*Todo, 0), CreatedAt: now, UpdatedAt: now}
}
func (l *TodoList) AddTodo(todo *Todo) { todo.Order = len(l.Todos); l.Todos = append(l.Todos, todo); l.UpdatedAt = time.Now() }
func (l *TodoList) RemoveTodo(id string) bool {
	for i, t := range l.Todos {
		if t.ID == id {
			l.Todos = append(l.Todos[:i], l.Todos[i+1:]...); l.reorder(); l.UpdatedAt = time.Now()
			return true
		}
	}
	return false
}
func (l *TodoList) GetTodo(id string) *Todo {
	for _, t := range l.Todos {
		if t.ID == id {
			return t
		}
	}
	return nil
}
func (l *TodoList) MoveTodo(id string, newIndex int) bool {
	if newIndex < 0 || newIndex >= len(l.Todos) {
		return false
	}
	var todo *Todo; var old int; found := false
	for i, t := range l.Todos {
		if t.ID == id {
			todo = t; old = i; found = true; break
		}
	}
	if !found {
		return false
	}
	l.Todos = append(l.Todos[:old], l.Todos[old+1:]...)
	if newIndex >= len(l.Todos) {
		l.Todos = append(l.Todos, todo)
	} else {
		l.Todos = append(l.Todos[:newIndex], append([]*Todo{todo}, l.Todos[newIndex:]...)...)
	}
	l.reorder(); l.UpdatedAt = time.Now()
	return true
}
func (l *TodoList) reorder() { for i, t := range l.Todos { t.Order = i } }
func (l *TodoList) GetPending() []*Todo {
	var r []*Todo; for _, t := range l.Todos { if t.Status == TodoStatusPending { r = append(r, t) } }; return r
}
func (l *TodoList) GetInProgress() []*Todo {
	var r []*Todo; for _, t := range l.Todos { if t.Status == TodoStatusInProgress { r = append(r, t) } }; return r
}
func (l *TodoList) GetDone() []*Todo {
	var r []*Todo; for _, t := range l.Todos { if t.Status == TodoStatusDone { r = append(r, t) } }; return r
}
func (l *TodoList) GetActive() []*Todo {
	var r []*Todo; for _, t := range l.Todos { if t.IsActive() { r = append(r, t) } }; return r
}
func (l *TodoList) Progress() (int, int) {
	done := 0; for _, t := range l.Todos { if t.IsDone() { done++ } }; return done, len(l.Todos)
}
func (l *TodoList) ProgressPercent() float64 {
	d, tot := l.Progress()
	if tot == 0 {
		return 0
	}
	return float64(d) / float64(tot) * 100
}
