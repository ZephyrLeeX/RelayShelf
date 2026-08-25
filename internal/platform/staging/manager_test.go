package staging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

func TestManagerCreatesLogicalFileAndOnlyScansOwnedNames(t *testing.T) {
	root := t.TempDir()
	manager, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.Must(uuid.NewV7())
	const logicalSize = int64(2 << 30)
	if err = manager.Create(id, logicalSize); err != nil {
		t.Fatal(err)
	}
	info, err := manager.Stat(id)
	if err != nil || info.Size() != logicalSize || info.Mode().Perm() != 0o600 {
		t.Fatalf("info=%v err=%v", info, err)
	}
	var stat unix.Stat_t
	if err = unix.Stat(filepath.Join(root, id.String()+suffix), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Blocks*512 >= logicalSize {
		t.Fatalf("file was eagerly allocated: blocks=%d", stat.Blocks)
	}
	if err = os.WriteFile(filepath.Join(root, "do-not-touch"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "not-a-uuid.upload"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned, err := manager.OwnedFiles()
	if err != nil || len(owned) != 1 || owned[0] != id {
		t.Fatalf("owned=%v err=%v", owned, err)
	}
	if err = manager.Delete(id); err != nil {
		t.Fatal(err)
	}
	if err = manager.Delete(id); err != nil {
		t.Fatal("delete must treat ENOENT as success")
	}
	if _, err = os.Stat(filepath.Join(root, "do-not-touch")); err != nil {
		t.Fatal("unknown file was touched")
	}
}

func TestManagerRejectsNilIDAndZeroLengthWorks(t *testing.T) {
	manager, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Create(uuid.Nil, 0); err != ErrInvalidUploadID {
		t.Fatalf("nil id: %v", err)
	}
	id := uuid.Must(uuid.NewV7())
	if err = manager.Create(id, 0); err != nil {
		t.Fatal(err)
	}
	info, err := manager.Stat(id)
	if err != nil || info.Size() != 0 {
		t.Fatalf("zero file=%v %v", info, err)
	}
}
