package domain

import "time"

// Checkpoint es un snapshot del workspace tomado antes de que el agente realice
// modificaciones (modo build). Permite revertir iteraciones fallidas sin
// depender de Git manual (/undo, forgen undo).
type Checkpoint struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Workspace  string    `json:"workspace"`
	CreatedAt  time.Time `json:"created_at"`
	FileCount  int       `json:"file_count"`
	TotalBytes int64     `json:"total_bytes"`
}
