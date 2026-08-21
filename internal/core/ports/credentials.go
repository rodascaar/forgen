// Package ports define las interfaces que el dominio y los casos de uso
// necesitan. Los adapters implementan estos puertos.
package ports

import "context"

// CredentialStore guarda y recupera secretos (API keys) de forma segura.
// El resto de forgen jamás conoce dónde vive el secreto: solo pide por clave.
//
// Las implementaciones deben priorizar el almacén seguro del sistema
// operativo (Keychain, Secret Service, Credential Manager) y degradar a un
// archivo local con permisos restrictivos cuando no haya almacén disponible.
type CredentialStore interface {
	// Set guarda un secreto bajo una clave.
	Set(ctx context.Context, key, secret string) error
	// Get recupera un secreto por clave. Devuelve error si no existe.
	Get(ctx context.Context, key string) (string, error)
	// Delete elimina un secreto por clave.
	Delete(ctx context.Context, key string) error
}
