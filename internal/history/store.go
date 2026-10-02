package history

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Store persists observations by opaque stream identity, independent of file paths.
// List returns a non-nil empty slice for a missing stream and reports corruption.
// Events are ordered oldest first. Rewriting the same timestamp/ID replaces that
// observation, matching the existing file format. Delete is retryable and succeeds
// for a missing stream. Callers serialize mutations and own deduplication, retention,
// and suppression of late writes after purge; Delete is not a multi-store transaction.
// Implementations must not retain caller-owned payload buffers after Write returns.
type Store interface {
	Write(stream string, event Event) error
	List(stream string) ([]Event, error)
	Delete(stream string) error
}

// FileStore preserves the existing hashed directories and JSON event format.
// Root is a trusted application-selected retained-data location.
type FileStore struct{ Root string }

var _ Store = FileStore{}

// Dir maps opaque stream IDs to the legacy directory layout, including IDs that
// contain slashes or other characters which must never be interpreted as paths.
func (s FileStore) Dir(stream string) string {
	sum := sha256.Sum256([]byte(stream))
	return filepath.Join(s.Root, hex.EncodeToString(sum[:]))
}

func (s FileStore) Write(stream string, event Event) error { return WriteEvent(s.Dir(stream), event) }
func (s FileStore) List(stream string) ([]Event, error)    { return ReadEvents(s.Dir(stream)) }
func (s FileStore) Delete(stream string) error             { return os.RemoveAll(s.Dir(stream)) }
