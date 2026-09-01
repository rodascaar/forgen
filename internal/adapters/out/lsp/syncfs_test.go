package lsp

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// innerFS es un FileSystem mínimo en memoria.
type innerFS struct {
	files map[string][]byte
}

func (f *innerFS) Read(_ context.Context, path string) ([]byte, error) { return f.files[path], nil }
func (f *innerFS) Write(_ context.Context, path string, data []byte) error {
	f.files[path] = append([]byte(nil), data...)
	return nil
}
func (f *innerFS) Exists(context.Context, string) (bool, error) { return true, nil }
func (f *innerFS) IsDir(context.Context, string) (bool, error) { return false, nil }
func (f *innerFS) Glob(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *innerFS) Search(context.Context, string, string, string) ([]ports.SearchMatch, error) {
	return nil, nil
}

type recordingSyncer struct {
	synced []string
}

func (r *recordingSyncer) SyncDocument(_ context.Context, path string) error {
	r.synced = append(r.synced, path)
	return nil
}

func TestSyncingFileSystemNotifiesOnWrite(t *testing.T) {
	inner := &innerFS{files: map[string][]byte{}}
	syncer := &recordingSyncer{}
	wrapped := NewSyncingFileSystem(inner, syncer)

	if err := wrapped.Write(context.Background(), "a.go", []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(syncer.synced) != 1 || syncer.synced[0] != "a.go" {
		t.Fatalf("synced = %v", syncer.synced)
	}
	if string(inner.files["a.go"]) != "x" {
		t.Fatalf("no se escribió al inner")
	}
}

func TestSyncingFileSystemDelegatesRead(t *testing.T) {
	inner := &innerFS{files: map[string][]byte{"a.go": []byte("hola")}}
	wrapped := NewSyncingFileSystem(inner, nil)

	data, err := wrapped.Read(context.Background(), "a.go")
	if err != nil || string(data) != "hola" {
		t.Fatalf("Read = %q err=%v", data, err)
	}
}
