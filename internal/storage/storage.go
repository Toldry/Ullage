// Package storage reads and writes Ullage's files in the user's config directory.
package storage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config holds non-secret preferences. It never contains scan results.
type Config struct {
	Theme                string   `json:"theme"`
	IncludeOtherProfiles bool     `json:"includeOtherProfiles"`
	Exclusions           []string `json:"exclusions"`
	// Prompt tuning; zero means the built-in default.
	PromptMinimumMegabytes int `json:"promptMinimumMegabytes,omitempty"`
	PromptBatchLines       int `json:"promptBatchLines,omitempty"`
	PromptMaximumItems     int `json:"promptMaximumItems,omitempty"`
}

const configFileName = "config.json"

// ConfigExists reports whether preferences were ever saved, which marks a first run.
func ConfigExists(directory string) bool {
	_, err := os.Stat(filepath.Join(directory, configFileName))
	return err == nil
}

// LoadConfig returns the saved config, or defaults when none exists yet.
func LoadConfig(directory string) (Config, error) {
	content, err := os.ReadFile(filepath.Join(directory, configFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Exclusions: []string{}}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, err
	}
	if config.Exclusions == nil {
		config.Exclusions = []string{}
	}
	return config, nil
}

// SaveConfig writes the config atomically.
func SaveConfig(directory string, config Config) error {
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomically(filepath.Join(directory, configFileName), content)
}

// WriteFileAtomically replaces path so readers see either the old or the new content,
// never a torn write, even if the process dies midway.
func WriteFileAtomically(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
