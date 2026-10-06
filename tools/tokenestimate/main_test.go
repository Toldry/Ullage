package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{
		"":                      0,
		"the":                   1,
		"categories":            2,
		" 12 6.1G Temp":         1 + 1 + 1 + 1 + 1 + 1,
		"@SESSION 7KQX2M":       1 + 2 + 1 + 1 + 1 + 1,
		"node_modules x31 [9k]": 1 + 1 + 2 + 1 + 1 + 1 + 1 + 1 + 1,
	}
	for text, want := range cases {
		if got := EstimateTokens(text); got != want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", text, got, want)
		}
	}
}

// The estimator must never make network calls (spec): it may not import any package
// that can open a connection.
func TestEstimatorImportsNoNetworking(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path, _ := strconv.Unquote(imported.Path.Value)
			if path == "net" || strings.HasPrefix(path, "net/") || path == "crypto/tls" ||
				path == "os/exec" {
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
}

func TestSampleRuns(t *testing.T) {
	if err := run("", false); err != nil {
		t.Fatal(err)
	}
}
