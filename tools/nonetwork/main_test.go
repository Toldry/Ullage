package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepositoryHasNoOutboundNetworkUse runs the check over the real repository, so
// `go test ./...` enforces safety rule 1.
func TestRepositoryHasNoOutboundNetworkUse(t *testing.T) {
	findings, err := check(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, found := range findings {
		t.Errorf("%s: %s", found.location, found.message)
	}
}

func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCatchesViolations(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module x\n\nrequire (\n\tevil.example/telemetry v1.0.0\n)\n")
	writeFixture(t, root, "internal/a/a.go", `package a
import (
	"net"
	web "net/http"
	_ "net/smtp"
)
func f() {
	web.Get("https://telemetry.example")
	net.Dial("tcp", "example.com:80")
	net.Listen("tcp", "0.0.0.0:8080")
	_ = web.Client{}
}`)
	writeFixture(t, root, "internal/a/a_test.go", `package a
import "net/http"
func g() { http.Get("http://ok-in-tests") }`)
	writeFixture(t, root, "web/src/app.css",
		`@import url("https://fonts.googleapis.com/css2?family=IBM+Plex+Sans");`)
	writeFixture(t, root, "web/src/ok.ts",
		`const ns = "http://www.w3.org/2000/svg"; const local = "http://127.0.0.1:1";`)

	findings, err := check(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, found := range findings {
		joined += found.location + " " + found.message + "\n"
	}
	for _, expected := range []string{"net/smtp", "net/http.Get", "net.Dial", "net listeners",
		"net/http.Client", "evil.example/telemetry", "fonts.googleapis.com"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("missing finding for %s in:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "a_test.go") || strings.Contains(joined, "ok.ts") {
		t.Errorf("false positive:\n%s", joined)
	}
}
