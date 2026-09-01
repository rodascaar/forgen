package lsp

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// DocumentSyncer notifica cambios de documentos a un listener externo.
type DocumentSyncer interface {
	SyncDocument(ctx context.Context, path string) error
}

// SyncingFileSystem envuelve un FileSystem y notifica tras cada escritura.
type SyncingFileSystem struct {
	inner    ports.FileSystem
	onChange DocumentSyncer
}

// NewSyncingFileSystem construye el decorador de sincronización.
func NewSyncingFileSystem(inner ports.FileSystem, onChange DocumentSyncer) *SyncingFileSystem {
	return &SyncingFileSystem{inner: inner, onChange: onChange}
}

// Write implementa ports.FileSystem y notifica el cambio.
func (s *SyncingFileSystem) Write(ctx context.Context, path string, data []byte) error {
	if err := s.inner.Write(ctx, path, data); err != nil {
		return err
	}
	if s.onChange != nil {
		_ = s.onChange.SyncDocument(ctx, path)
	}
	return nil
}

func (s *SyncingFileSystem) Read(ctx context.Context, path string) ([]byte, error) {
	return s.inner.Read(ctx, path)
}

func (s *SyncingFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	return s.inner.Exists(ctx, path)
}

func (s *SyncingFileSystem) IsDir(ctx context.Context, path string) (bool, error) {
	return s.inner.IsDir(ctx, path)
}

func (s *SyncingFileSystem) Glob(ctx context.Context, pattern string) ([]string, error) {
	return s.inner.Glob(ctx, pattern)
}

func (s *SyncingFileSystem) Search(ctx context.Context, root, query, include string) ([]ports.SearchMatch, error) {
	return s.inner.Search(ctx, root, query, include)
}

var _ ports.FileSystem = (*SyncingFileSystem)(nil)
