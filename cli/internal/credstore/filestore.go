package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type FileStore struct {
	path string
}

// NewFileStore returns a new filestore pathed to system user config directory or pathOverride.
func NewFileStore(pathOverride string) (*FileStore, error) {
	if pathOverride != "" {
		return &FileStore{path: pathOverride}, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user config directory: %w", err)
	}
	return &FileStore{path: filepath.Join(dir, "invosit", "credentials.json")}, nil
}

func (s *FileStore) Path() string { return s.path }

// Load returns credentials stored in invosit/credentials.json
func (s *FileStore) Load() (Credentials, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to open %s: %w", s.path, err)
	}
	defer func() { _ = f.Close() }()

	// POSIX perm check. Windows ACLs don't map to mode bits.
	if runtime.GOOS != "windows" {
		info, err := f.Stat()
		if err != nil {
			return Credentials{}, fmt.Errorf("failed to retrieve stat %s: %w", s.path, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			return Credentials{}, fmt.Errorf("%w: %s has mode %#o", ErrInsecurePerms, s.path, perm)
		}
	}

	var c Credentials
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return Credentials{}, fmt.Errorf("failed to decode credentials at %s: %w", s.path, err)
	}
	return c, nil
}

// Save stores credentials to invosit/credentials.json. Overwrites existing credential file
func (s *FileStore) Save(c Credentials) error {
	if c.Version == 0 {
		c.Version = SchemaVersion
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode credentials: %w", err)
	}

	// Write-temp + rename. Makes the swap atomic, so a crash mid-write can't
	// leave a half-truncated credentials file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to rename %s: %w", tmp, err)
	}
	return nil
}

// Clear deletes invosit/credentials.json if it exists
func (s *FileStore) Clear() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove %s: %w", s.path, err)
	}
	return nil
}
