package storage

import (
	"reflect"
	"testing"
)

func TestConfigRoundTripAndDefaults(t *testing.T) {
	directory := t.TempDir()
	defaults, err := LoadConfig(directory)
	if err != nil || defaults.Exclusions == nil {
		t.Fatalf("defaults = %+v, %v", defaults, err)
	}
	saved := Config{Theme: "light", IncludeOtherProfiles: true, Exclusions: []string{"*.kdbx"}}
	if err := SaveConfig(directory, saved); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(directory)
	if err != nil || !reflect.DeepEqual(loaded, saved) {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}
}
