//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DataDirectory returns ~/.local/share/ullage.
func DataDirectory() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "ullage"), nil
}

// ShowError reports a fatal problem on standard error.
func ShowError(message string) {
	fmt.Fprintln(os.Stderr, "ullage:", message)
}

// InstallShortcut is not implemented on Linux yet.
func InstallShortcut(string) error {
	return errors.New("shortcuts are only created on Windows")
}

// RemoveShortcut is not implemented on Linux yet.
func RemoveShortcut() error { return nil }
