package store_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alswl/excalidraw-storage-backend-go/internal/store"
)

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})
	return st
}

func TestSetGet(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	value := []byte(`{"type":"excalidraw"}`)
	if err := st.Set(ctx, store.Scenes, "scene-1", value); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	got, err := st.Get(ctx, store.Scenes, "scene-1")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("Get() = %q, want %q", got, value)
	}
}

func TestSetOverwrites(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	_ = st.Set(ctx, store.Rooms, "room-1", []byte("first"))
	if err := st.Set(ctx, store.Rooms, "room-1", []byte("second")); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	got, err := st.Get(ctx, store.Rooms, "room-1")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if string(got) != "second" {
		t.Errorf("Get() = %q, want %q", got, "second")
	}
}

func TestGetNotFound(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))

	_, err := st.Get(context.Background(), store.Files, "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() error = %v, want store.ErrNotFound", err)
	}
}

func TestExists(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	exists, err := st.Exists(ctx, store.Files, "a")
	if err != nil {
		t.Fatalf("Exists(): %v", err)
	}
	if exists {
		t.Fatal("Exists() = true for missing key")
	}

	if err := st.Set(ctx, store.Files, "a", []byte("x")); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	exists, err = st.Exists(ctx, store.Files, "a")
	if err != nil {
		t.Fatalf("Exists(): %v", err)
	}
	if !exists {
		t.Fatal("Exists() = false for existing key")
	}
}

func TestCreate(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	created, err := st.Create(ctx, store.Scenes, "123", []byte("first"))
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if !created {
		t.Fatal("Create() = false for new key")
	}

	created, err = st.Create(ctx, store.Scenes, "123", []byte("second"))
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if created {
		t.Fatal("Create() = true for existing key")
	}

	got, err := st.Get(ctx, store.Scenes, "123")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if string(got) != "first" {
		t.Errorf("Get() = %q, want the original value %q", got, "first")
	}
}

func TestEmptyValue(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	if err := st.Set(ctx, store.Scenes, "empty", nil); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	got, err := st.Get(ctx, store.Scenes, "empty")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Get() = %q, want empty value", got)
	}

	exists, err := st.Exists(ctx, store.Scenes, "empty")
	if err != nil {
		t.Fatalf("Exists(): %v", err)
	}
	if !exists {
		t.Error("Exists() = false for empty value")
	}
}

func TestNamespacesAreIsolated(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "test.db"))
	ctx := context.Background()

	namespaces := []store.Namespace{store.Scenes, store.Rooms, store.Files}
	for i, ns := range namespaces {
		if err := st.Set(ctx, ns, "same-id", []byte{byte(i)}); err != nil {
			t.Fatalf("Set(%s): %v", ns, err)
		}
	}

	for i, ns := range namespaces {
		got, err := st.Get(ctx, ns, "same-id")
		if err != nil {
			t.Fatalf("Get(%s): %v", ns, err)
		}
		if len(got) != 1 || got[0] != byte(i) {
			t.Errorf("Get(%s) = %v, want [%d]", ns, got, i)
		}
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")

	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	if err := first.Set(context.Background(), store.Files, "file-1", []byte("image")); err != nil {
		t.Fatalf("Set(): %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	second := openStore(t, path)
	got, err := second.Get(context.Background(), store.Files, "file-1")
	if err != nil {
		t.Fatalf("Get() after reopen: %v", err)
	}
	if string(got) != "image" {
		t.Errorf("Get() after reopen = %q, want %q", got, "image")
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "test.db")
	st := openStore(t, path)
	if err := st.Set(context.Background(), store.Scenes, "k", []byte("v")); err != nil {
		t.Fatalf("Set(): %v", err)
	}
}

func TestOpenEmptyPath(t *testing.T) {
	if _, err := store.Open(""); err == nil {
		t.Fatal("Open(\"\") = nil error, want error")
	}
}
