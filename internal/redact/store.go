package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/toldry/ullage/internal/protect"
	"github.com/toldry/ullage/internal/storage"
)

// Document is everything persisted in the encrypted rules file.
type Document struct {
	Version         int            `json:"version"`
	Rules           []Rule         `json:"rules"`
	Placeholders    map[string]int `json:"placeholders"`
	NextPlaceholder int            `json:"nextPlaceholder"`
}

const (
	storeFileName  = "rules.dpapi"
	storeMagic     = "ULLAGE-RULES\x00"
	currentVersion = 1
)

// ErrUnavailable means the rules file exists but cannot be used. Prompt generation must
// stop until the user resolves it (safety rule 7).
var ErrUnavailable = errors.New("redaction rules cannot be read")

// StorePath returns the location of the rules file.
func StorePath(configDirectory string) string {
	return filepath.Join(configDirectory, storeFileName)
}

// Load reads the rules file. A missing file is an empty document; any other problem
// returns an error wrapping ErrUnavailable and never a partial document.
func Load(configDirectory string, protector protect.Protector) (Document, error) {
	sealed, err := os.ReadFile(StorePath(configDirectory))
	if errors.Is(err, fs.ErrNotExist) {
		return Document{Version: currentVersion, Placeholders: map[string]int{},
			NextPlaceholder: 1}, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if !bytes.HasPrefix(sealed, []byte(storeMagic)) {
		return Document{}, fmt.Errorf("%w: not an Ullage rules file", ErrUnavailable)
	}
	plain, err := protector.Unprotect(sealed[len(storeMagic):])
	if err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if document.Version != currentVersion {
		return Document{}, fmt.Errorf("%w: unsupported version %d", ErrUnavailable,
			document.Version)
	}
	for _, rule := range document.Rules {
		if err := rule.Validate(); err != nil {
			return Document{}, fmt.Errorf("%w: rule %s: %v", ErrUnavailable, rule.ID, err)
		}
	}
	if document.Placeholders == nil {
		document.Placeholders = map[string]int{}
	}
	return document, nil
}

// Save encrypts and writes the document atomically.
func Save(configDirectory string, protector protect.Protector, document Document) error {
	document.Version = currentVersion
	plain, err := json.Marshal(document)
	if err != nil {
		return err
	}
	sealed, err := protector.Protect(plain)
	if err != nil {
		return err
	}
	return storage.WriteFileAtomically(StorePath(configDirectory),
		append([]byte(storeMagic), sealed...))
}
