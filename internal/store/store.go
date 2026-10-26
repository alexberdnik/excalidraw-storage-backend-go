package store

import (
	"context"
	"errors"
)

type Namespace string

const (
	Scenes Namespace = "SCENES"
	Rooms  Namespace = "ROOMS"
	Files  Namespace = "FILES"
)

var ErrNotFound = errors.New("store: key not found")

type Store interface {
	// Get returns the value stored under (namespace, key).
	// It returns ErrNotFound (wrapped) when the key does not exist.
	Get(ctx context.Context, namespace Namespace, key string) ([]byte, error)

	// Exists reports whether the key exists.
	Exists(ctx context.Context, namespace Namespace, key string) (bool, error)

	// Set inserts or overwrites the value stored under (namespace, key).
	Set(ctx context.Context, namespace Namespace, key string, value []byte) error

	// Create inserts the key only if it does not exist yet.
	// The returned bool is false when the key already existed.
	Create(ctx context.Context, namespace Namespace, key string, value []byte) (bool, error)

	// Close releases the resources held by the store.
	Close() error
}

// valid reports whether the namespace is one of the known namespaces.
// It keeps the file backend from creating directories for arbitrary names.
func (n Namespace) valid() bool {
	switch n {
	case Scenes, Rooms, Files:
		return true
	}
	return false
}
