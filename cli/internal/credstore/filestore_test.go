package credstore_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kyenel64/invosit/cli/internal/credstore"
)

func newStore(t *testing.T) *credstore.FileStore {
	t.Helper()
	s, err := credstore.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func sampleCreds() credstore.Credentials {
	return credstore.Credentials{
		Email:        "test@example.com",
		UserID:       "usr_abc123",
		SessionToken: "ory_st_test",
		KratosURL:    "http://localhost:4433",
		APIURL:       "http://localhost:8080",
		SavedAt:      time.Now().UTC().Truncate(time.Second),
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	s := newStore(t)
	in := sampleCreds()

	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Email != in.Email {
		t.Errorf("Email = %q, want %q", out.Email, in.Email)
	}
	if out.SessionToken != in.SessionToken {
		t.Errorf("SessionToken differs")
	}
	if out.Version != credstore.SchemaVersion {
		t.Errorf("Version = %d, want %d", out.Version, credstore.SchemaVersion)
	}
}

func TestLoadMissing(t *testing.T) {
	s := newStore(t)
	_, err := s.Load()
	if !errors.Is(err, credstore.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestLoadInsecurePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	s := newStore(t)
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Chmod(s.Path(), 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	_, err := s.Load()
	if !errors.Is(err, credstore.ErrInsecurePerms) {
		t.Errorf("want ErrInsecurePerms, got %v", err)
	}
}

func TestSavePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	s := newStore(t)
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %#o, want 0600", got)
	}
}

func TestSaveOverwrites(t *testing.T) {
	s := newStore(t)
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second := sampleCreds()
	second.Email = "different@example.com"
	if err := s.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Email != "different@example.com" {
		t.Errorf("Email = %q, want overwrite", out.Email)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "deeply", "nested", "credentials.json")
	s, err := credstore.NewFileStore(nested)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("Save should create parent dirs: %v", err)
	}
}

func TestSaveLeavesNoTmpFile(t *testing.T) {
	s := newStore(t)
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(s.Path() + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tmp file should be cleaned up after rename")
	}
}

func TestClearExisting(t *testing.T) {
	s := newStore(t)
	if err := s.Save(sampleCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(s.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still exists after Clear")
	}
}

func TestClearMissingIsNoop(t *testing.T) {
	s := newStore(t)
	if err := s.Clear(); err != nil {
		t.Errorf("Clear on missing should be a no-op, got %v", err)
	}
}
