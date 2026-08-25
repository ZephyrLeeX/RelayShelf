package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Key string

func ObjectKey(id uuid.UUID) Key     { return Key("objects/" + id.String()) }
func DerivativeKey(id uuid.UUID) Key { return Key("derivatives/" + id.String()) }
func CommitTempKey(id uuid.UUID) Key { return Key(".commit-tmp/" + id.String() + ".tmp") }

func validateKey(key Key) error {
	s := string(key)
	if s == "" || filepath.IsAbs(s) || filepath.Clean(s) != s || strings.Contains(s, "\\") || s == "." || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") {
		return ErrInvalidKey
	}
	first, rest, ok := strings.Cut(s, "/")
	if !ok || rest == "" || (first != "objects" && first != "derivatives" && first != ".commit-tmp") {
		return ErrInvalidKey
	}
	return nil
}

func parseInternalTempName(name string) (uuid.UUID, bool) {
	if !strings.HasSuffix(name, ".tmp") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSuffix(name, ".tmp"))
	return id, err == nil
}

func (k Key) String() string   { return string(k) }
func (k Key) Validate() error  { return validateKey(k) }
func (k Key) GoString() string { return fmt.Sprintf("storage.Key(%q)", string(k)) }
