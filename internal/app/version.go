package app

// Version es la versión actual de forgen (semver).
// Se puede inyectar en build con:
//
//	go build -ldflags "-X github.com/rodascaar/forgen/internal/app.Version=v1.2.3"
var Version = "0.1.5"

// Commit es el hash de git del build (inyectado vía ldflags).
var Commit = "dev"
