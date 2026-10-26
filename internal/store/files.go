package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	blobSuffix    = ".blob"
	tempPrefix    = ".tmp-"
	maxNameLength = 120
)

// FileStore keeps one plain file per blob:
//
//	<root>/<NAMESPACE>/<shard>/<key>.blob
//
// where <shard> is the last two characters of the key, e.g. the scene
// 12345 lives in <root>/SCENES/45/12345.blob. The extra folder level keeps
// a namespace with many keys from turning into one huge directory.
//
// Keys are not guaranteed to be numeric: the server generates 16 digit ids
// for scenes, but room and file ids are chosen by the Excalidraw client and
// are usually random alphanumeric strings. Every key is therefore encoded
// into a safe file name (see encodeKey) instead of being used verbatim.
type FileStore struct {
	root string
}

func OpenFiles(root string) (*FileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("store: empty file store path")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("store: resolve file store path: %w", err)
	}

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("store: create file store: %w", err)
	}

	return &FileStore{root: abs}, nil
}

func (s *FileStore) Root() string {
	return s.root
}

func (s *FileStore) Close() error {
	return nil
}

// Get returns the value stored under (namespace, key).
// It returns ErrNotFound (wrapped) when the key does not exist.
func (s *FileStore) Get(ctx context.Context, namespace Namespace, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path, err := s.blobPath(namespace, key)
	if err != nil {
		return nil, err
	}

	value, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("store: %s/%s: %w", namespace, key, ErrNotFound)
	case err != nil:
		return nil, fmt.Errorf("store: get %s/%s: %w", namespace, key, err)
	}

	return value, nil
}

// Exists reports whether the key exists.
func (s *FileStore) Exists(ctx context.Context, namespace Namespace, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	path, err := s.blobPath(namespace, key)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: exists %s/%s: %w", namespace, key, err)
	}
	return !info.IsDir(), nil
}

// Set inserts or overwrites the value stored under (namespace, key).
func (s *FileStore) Set(ctx context.Context, namespace Namespace, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.blobPath(namespace, key)
	if err != nil {
		return err
	}

	if _, err := writeFile(path, value, false); err != nil {
		return fmt.Errorf("store: set %s/%s: %w", namespace, key, err)
	}
	return nil
}

// Create inserts the key only if it does not exist yet.
// The returned bool is false when the key already existed.
func (s *FileStore) Create(ctx context.Context, namespace Namespace, key string, value []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	path, err := s.blobPath(namespace, key)
	if err != nil {
		return false, err
	}

	created, err := writeFile(path, value, true)
	if err != nil {
		return false, fmt.Errorf("store: create %s/%s: %w", namespace, key, err)
	}
	return created, nil
}

func (s *FileStore) blobPath(namespace Namespace, key string) (string, error) {
	if !namespace.valid() {
		return "", fmt.Errorf("store: invalid namespace %q", namespace)
	}
	if key == "" {
		return "", fmt.Errorf("store: %s: empty key", namespace)
	}
	return filepath.Join(s.root, string(namespace), shard(key), blobName(key)), nil
}

// shard returns the two character directory name derived from the last two
// characters of the key (`12345` -> `45`). Characters outside [0-9a-z] map to
// '_' and upper case letters are folded to lower case.
func shard(key string) string {
	var b [2]byte

	switch len(key) {
	case 0:
		return "__"
	case 1:
		b[0], b[1] = '_', key[0]
	default:
		b[0], b[1] = key[len(key)-2], key[len(key)-1]
	}

	for i, c := range b {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z':
			b[i] = c
		case c >= 'A' && c <= 'Z':
			b[i] = c + ('a' - 'A')
		default:
			b[i] = '_'
		}
	}
	return string(b[:])
}

// blobName encodes the key into a file name. Alphanumerics plus '-' and '_'
// are kept as they are (this covers scene ids and the ids Excalidraw
// generates), every other byte becomes %XX.
func blobName(key string) string {
	encoded := encodeKey(key)
	if len(encoded) > maxNameLength {
		sum := sha256.Sum256([]byte(key))
		encoded = "sha256-" + hex.EncodeToString(sum[:])
	}
	return encoded + blobSuffix
}

func encodeKey(key string) string {
	const upperHex = "0123456789ABCDEF"

	var b strings.Builder
	b.Grow(len(key) + 8)
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0f])
		}
	}
	return b.String()
}

// writeFile writes value to path atomically. The temporary file lives in the
// destination directory so the rename stays on one file system.
func writeFile(path string, value []byte, exclusive bool) (bool, error) {
	tmp, err := writeTemp(filepath.Dir(path), value)
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp)

	if exclusive {
		if err := os.Link(tmp, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}

func writeTemp(dir string, value []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	file, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return "", err
	}
	name := file.Name()

	if _, err := file.Write(value); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
