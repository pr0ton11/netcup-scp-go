package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadRefreshToken reads a refresh token from a file, such as a Podman secret mounted
// at /run/secrets/<name>. Surrounding whitespace is ignored.
func ReadRefreshToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("%s: empty refresh token", path)
	}
	return tok, nil
}

// WriteRefreshToken stores a refresh token in a file readable only by its owner. It writes a
// temporary file and renames it, so a crash never leaves a partly written token behind.
func WriteRefreshToken(path, refreshToken string) error {
	if refreshToken == "" {
		return errors.New("empty refresh token")
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(refreshToken + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
