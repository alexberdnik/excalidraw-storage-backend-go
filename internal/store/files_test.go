package store_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/store"
)

func openFileStore(t *testing.T) (*store.FileStore, string) {
	t.Helper()

	root := filepath.Join(t.TempDir(), "blobs")
	st, err := store.OpenFiles(root)
	if err != nil {
		t.Fatalf("store.OpenFiles(%q): %v", root, err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})
	return st, root
}

// walkFiles returns every regular file below root, relative to root.
func walkFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", root, err)
	}
	return files
}

func TestFileStoreLayout(t *testing.T) {
	st, root := openFileStore(t)
	ctx := context.Background()

	value := []byte(`{"type":"excalidraw"}`)
	if err := st.Set(ctx, store.Scenes, "12345", value); err != nil {
		t.Fatalf("Set(): %v", err)
	}

	// namespace folder / two last characters of the id / <id>.blob
	want := filepath.Join(root, "SCENES", "45", "12345.blob")
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected blob at %q: %v (files: %v)", want, err, walkFiles(t, root))
	}
	if string(got) != string(value) {
		t.Errorf("blob content = %q, want %q", got, value)
	}

	// The file is the only one in the store: no temporary files are left.
	if files := walkFiles(t, root); len(files) != 1 || files[0] != "SCENES/45/12345.blob" {
		t.Errorf("store files = %v, want [SCENES/45/12345.blob]", files)
	}

	// The same bytes come back through the API.
	back, err := st.Get(ctx, store.Scenes, "12345")
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if string(back) != string(value) {
		t.Errorf("Get() = %q, want %q", back, value)
	}
}

func TestFileStoreCreatesNamespaceFolders(t *testing.T) {
	st, root := openFileStore(t)
	ctx := context.Background()

	namespaces := []store.Namespace{store.Scenes, store.Rooms, store.Files}
	for _, ns := range namespaces {
		if err := st.Set(ctx, ns, "abc", []byte(ns)); err != nil {
			t.Fatalf("Set(%s): %v", ns, err)
		}
	}

	for _, ns := range namespaces {
		dir := filepath.Join(root, string(ns))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("namespace folder %q: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestFileStoreShardFolder(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"12345", "45"},
		{"1234567890123456", "56"},
		{"abc", "bc"},
		{"a", "_a"},
		{"ABC", "bc"},
		{"room-1", "_1"},
		{"image_1", "_1"},
	}
	for _, tt := range tests {
		st, root := openFileStore(t)
		if err := st.Set(context.Background(), store.Files, tt.key, []byte("x")); err != nil {
			t.Fatalf("Set(%q): %v", tt.key, err)
		}
		shardDir := filepath.Join(root, "FILES", tt.want)
		if info, err := os.Stat(shardDir); err != nil || !info.IsDir() {
			t.Errorf("key %q: shard folder %q missing (err %v)", tt.key, shardDir, err)
		}
	}
}

// Keys that would be unsafe as file names must stay inside the store and must
// not collide with each other.
func TestFileStoreUnsafeKeysStayInside(t *testing.T) {
	st, root := openFileStore(t)
	ctx := context.Background()

	keys := []string{"../../etc/passwd", "../escape", ".", "..", "a/b", `c\d`, "with space"}
	for i, key := range keys {
		if err := st.Set(ctx, store.Files, key, []byte{byte(i)}); err != nil {
			t.Fatalf("Set(%q): %v", key, err)
		}
	}

	parent := filepath.Dir(root)
	if entries, err := os.ReadDir(parent); err == nil {
		for _, entry := range entries {
			if entry.Name() != filepath.Base(root) {
				t.Errorf("unexpected entry %q next to the store root", entry.Name())
			}
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

	for _, file := range walkFiles(t, root) {
		if !strings.HasSuffix(file, ".blob") {
			t.Errorf("unexpected non-blob file %q", file)
		}
	}
}

func TestFileStoreLongKey(t *testing.T) {
	st, root := openFileStore(t)
	longKey := strings.Repeat("k", 300)

	if err := st.Set(context.Background(), store.Files, longKey, []byte("x")); err != nil {
		t.Fatalf("Set(long key): %v", err)
	}

	var blob string
	for _, file := range walkFiles(t, root) {
		if strings.HasSuffix(file, ".blob") {
			blob = file
		}
	}
	if blob == "" {
		t.Fatal("no blob file written for the long key")
	}
	name := filepath.Base(blob)
	if !strings.HasPrefix(name, "sha256-") {
		t.Errorf("long key blob = %q, want a sha256- name", name)
	}
	if len(name) > 255 {
		t.Errorf("blob name length = %d, want at most 255", len(name))
	}

	got, err := st.Get(context.Background(), store.Files, longKey)
	if err != nil {
		t.Fatalf("Get(long key): %v", err)
	}
	if string(got) != "x" {
		t.Errorf("Get(long key) = %q, want %q", got, "x")
	}
}

func TestFileStoreOverwriteKeepsSingleFile(t *testing.T) {
	st, root := openFileStore(t)
	ctx := context.Background()

	for _, value := range []string{"first", "second", "third"} {
		if err := st.Set(ctx, store.Rooms, "room-1", []byte(value)); err != nil {
			t.Fatalf("Set(%q): %v", value, err)
		}
	}

	files := walkFiles(t, root)
	if len(files) != 1 || files[0] != "ROOMS/_1/room-1.blob" {
		t.Errorf("store files = %v, want [ROOMS/_1/room-1.blob]", files)
	}
}

func TestFileStoreCreateIsExclusive(t *testing.T) {
	st, root := openFileStore(t)
	ctx := context.Background()

	created, err := st.Create(ctx, store.Scenes, "12345", []byte("first"))
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if !created {
		t.Fatal("Create() = false for a new key")
	}

	created, err = st.Create(ctx, store.Scenes, "12345", []byte("second"))
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	if created {
		t.Fatal("Create() = true for an existing key")
	}

	// Only the winning write is on disk, and the losing temporary file is gone.
	if files := walkFiles(t, root); len(files) != 1 || files[0] != "SCENES/45/12345.blob" {
		t.Errorf("store files = %v, want [SCENES/45/12345.blob]", files)
	}
	got, err := os.ReadFile(filepath.Join(root, "SCENES", "45", "12345.blob"))
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(got) != "first" {
		t.Errorf("blob = %q, want %q", got, "first")
	}
}

func TestFileStoreRoot(t *testing.T) {
	_, root := openFileStore(t)
	st, err := store.OpenFiles(root)
	if err != nil {
		t.Fatalf("OpenFiles(): %v", err)
	}
	if st.Root() != root {
		t.Errorf("Root() = %q, want %q", st.Root(), root)
	}
}
