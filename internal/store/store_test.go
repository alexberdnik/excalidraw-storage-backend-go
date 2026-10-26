package store_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/store"
)

type backend struct {
	name string
	open func(t *testing.T) store.Store
}

func backends() []backend {
	return []backend{
		{
			name: "sqlite",
			open: func(t *testing.T) store.Store {
				t.Helper()
				return openStore(t, func() (store.Store, error) {
					return store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
				})
			},
		},
		{
			name: "files",
			open: func(t *testing.T) store.Store {
				t.Helper()
				return openStore(t, func() (store.Store, error) {
					return store.OpenFiles(filepath.Join(t.TempDir(), "blobs"))
				})
			},
		},
	}
}

func openStore(t *testing.T, open func() (store.Store, error)) store.Store {
	t.Helper()
	st, err := open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})
	return st
}

func eachBackend(t *testing.T, fn func(t *testing.T, st store.Store)) {
	t.Helper()
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			fn(t, b.open(t))
		})
	}
}

func TestSetGet(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
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
	})
}

func TestSetOverwrites(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
		ctx := context.Background()

		if err := st.Set(ctx, store.Rooms, "room-1", []byte("first")); err != nil {
			t.Fatalf("Set(first): %v", err)
		}
		if err := st.Set(ctx, store.Rooms, "room-1", []byte("second")); err != nil {
			t.Fatalf("Set(second): %v", err)
		}

		got, err := st.Get(ctx, store.Rooms, "room-1")
		if err != nil {
			t.Fatalf("Get(): %v", err)
		}
		if string(got) != "second" {
			t.Errorf("Get() = %q, want %q", got, "second")
		}
	})
}

func TestGetNotFound(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
		_, err := st.Get(context.Background(), store.Files, "missing")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Get() error = %v, want store.ErrNotFound", err)
		}
	})
}

func TestExists(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
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
	})
}

func TestCreate(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
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
	})
}

func TestEmptyValue(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
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
	})
}

func TestBinaryValue(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
		ctx := context.Background()
		value := make([]byte, 256)
		for i := range value {
			value[i] = byte(i)
		}

		if err := st.Set(ctx, store.Files, "image-1", value); err != nil {
			t.Fatalf("Set(): %v", err)
		}
		got, err := st.Get(ctx, store.Files, "image-1")
		if err != nil {
			t.Fatalf("Get(): %v", err)
		}
		if !bytes.Equal(got, value) {
			t.Errorf("Get() = %v, want the original 256 byte payload", got)
		}
	})
}

func TestNamespacesAreIsolated(t *testing.T) {
	eachBackend(t, func(t *testing.T, st store.Store) {
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
	})
}

func TestUnusualKeys(t *testing.T) {
	keys := []string{
		"a",
		"0123456789abcdef0123456789ab",
		"room-1",
		"file_1",
		"a b",
		"dot.dot",
		"..",
		"../../etc/passwd",
		"with/slash",
		"with%25percent",
		"UPPER",
		"ünïcode",
	}

	eachBackend(t, func(t *testing.T, st store.Store) {
		ctx := context.Background()

		for i, key := range keys {
			value := []byte{byte(i)}
			if err := st.Set(ctx, store.Files, key, value); err != nil {
				t.Fatalf("Set(%q): %v", key, err)
			}
		}
		for i, key := range keys {
			got, err := st.Get(ctx, store.Files, key)
			if err != nil {
				t.Fatalf("Get(%q): %v", key, err)
			}
			if len(got) != 1 || got[0] != byte(i) {
				t.Errorf("Get(%q) = %v, want [%d]", key, got, i)
			}
		}
	})
}

func TestPersistenceAcrossReopen(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "persistent.db")

		first, err := store.OpenSQLite(path)
		if err != nil {
			t.Fatalf("OpenSQLite(): %v", err)
		}
		if err := first.Set(context.Background(), store.Files, "file-1", []byte("image")); err != nil {
			t.Fatalf("Set(): %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("Close(): %v", err)
		}

		second := openStore(t, func() (store.Store, error) { return store.OpenSQLite(path) })
		assertStored(t, second)
	})

	t.Run("files", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "blobs")

		first, err := store.OpenFiles(path)
		if err != nil {
			t.Fatalf("OpenFiles(): %v", err)
		}
		if err := first.Set(context.Background(), store.Files, "file-1", []byte("image")); err != nil {
			t.Fatalf("Set(): %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("Close(): %v", err)
		}

		second := openStore(t, func() (store.Store, error) { return store.OpenFiles(path) })
		assertStored(t, second)
	})
}

func assertStored(t *testing.T, st store.Store) {
	t.Helper()
	got, err := st.Get(context.Background(), store.Files, "file-1")
	if err != nil {
		t.Fatalf("Get() after reopen: %v", err)
	}
	if string(got) != "image" {
		t.Errorf("Get() after reopen = %q, want %q", got, "image")
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "dir", "test.db")
		st := openStore(t, func() (store.Store, error) { return store.OpenSQLite(path) })
		if err := st.Set(context.Background(), store.Scenes, "k", []byte("v")); err != nil {
			t.Fatalf("Set(): %v", err)
		}
	})

	t.Run("files", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "dir", "blobs")
		st := openStore(t, func() (store.Store, error) { return store.OpenFiles(path) })
		if err := st.Set(context.Background(), store.Scenes, "k", []byte("v")); err != nil {
			t.Fatalf("Set(): %v", err)
		}
	})
}

func TestOpenEmptyPath(t *testing.T) {
	if _, err := store.OpenSQLite(""); err == nil {
		t.Fatal("OpenSQLite(\"\") = nil error, want error")
	}
	if _, err := store.OpenFiles(""); err == nil {
		t.Fatal("OpenFiles(\"\") = nil error, want error")
	}
}
