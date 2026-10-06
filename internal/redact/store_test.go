package redact

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/protect"
)

func TestStoreRoundTripIsEncrypted(t *testing.T) {
	directory := t.TempDir()
	protector := protect.Native(directory)
	anchor := platform.FileID{Volume: 9, Low: 77}
	document := Document{
		Rules: []Rule{{ID: "a", Type: RuleExact, Pattern: "~/clients/northwind",
			Treatment: TreatmentSubtree, Anchor: &anchor,
			Created: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)}},
		Placeholders:    map[string]int{"northwind": 3},
		NextPlaceholder: 4,
	}
	if err := Save(directory, protector, document); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(StorePath(directory))
	if bytes.Contains(raw, []byte("northwind")) {
		t.Fatalf("rules file contains plaintext")
	}
	loaded, err := Load(directory, protector)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rules) != 1 || *loaded.Rules[0].Anchor != anchor ||
		loaded.Placeholders["northwind"] != 3 {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestMissingStoreIsEmpty(t *testing.T) {
	directory := t.TempDir()
	document, err := Load(directory, protect.Native(directory))
	if err != nil || len(document.Rules) != 0 {
		t.Fatalf("missing store: %+v %v", document, err)
	}
}

// Safety rule 7: any unreadable store must fail closed, never load as empty.
func TestUnreadableStoreFailsClosed(t *testing.T) {
	directory := t.TempDir()
	protector := protect.Native(directory)
	if err := Save(directory, protector, Document{Rules: []Rule{{ID: "a",
		Type: RuleName, Pattern: "*tax*", Treatment: TreatmentKeepExtension}}}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(StorePath(directory))

	tampered := append([]byte{}, original...)
	tampered[len(tampered)-1] ^= 1
	corruptions := map[string][]byte{
		"tampered":    tampered,
		"wrong magic": append([]byte("NOT-ULLAGE\x00"), original[len(storeMagic):]...),
		"empty":       {},
		"truncated":   original[:len(storeMagic)+3],
	}
	for name, content := range corruptions {
		if err := os.WriteFile(StorePath(directory), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(directory, protector); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v, want ErrUnavailable", name, err)
		}
	}

	invalidRule := Document{Rules: []Rule{{ID: "x", Type: "regex", Pattern: ".*",
		Treatment: TreatmentSubtree}}}
	if err := Save(directory, protector, invalidRule); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(directory, protector); !errors.Is(err, ErrUnavailable) {
		t.Errorf("invalid stored rule: err = %v", err)
	}
}
