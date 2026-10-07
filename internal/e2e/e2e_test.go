// Package e2e drives a real local server over loopback, the way the browser does: launch
// link, cookie, token header and Origin. It runs on Windows CI against the real Recycle
// Bin and on Linux against a temporary freedesktop Trash.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/toldry/ullage/internal/server"
	"github.com/toldry/ullage/internal/session"
	"github.com/toldry/ullage/internal/storage"
)

type client struct {
	t      *testing.T
	http   *http.Client
	base   string
	token  string
	origin string
}

func (client *client) call(method, path string, body any, into any) int {
	client.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	request, _ := http.NewRequest(method, client.base+path, reader)
	request.Header.Set("X-Ullage-Token", client.token)
	request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Origin", client.origin)
	}
	response, err := client.http.Do(request)
	if err != nil {
		client.t.Fatal(err)
	}
	defer response.Body.Close()
	content, _ := io.ReadAll(response.Body)
	if into != nil && response.StatusCode == http.StatusOK {
		if err := json.Unmarshal(content, into); err != nil {
			client.t.Fatalf("%s %s: %v\n%s", method, path, err, content)
		}
	}
	return response.StatusCode
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startServer(t *testing.T) (*client, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "projects", "app", "node_modules", "big.bin"), 3<<20)
	writeFile(t, filepath.Join(root, "Videos", "clip.mp4"), 3<<20)
	if runtime.GOOS != "windows" {
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, "..", "trash-data"))
	}

	environment := session.NativeEnvironment(t.TempDir())
	environment.HistoryPath = filepath.Join(t.TempDir(), "history.dpapi")
	manager, err := session.NewManager(environment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpdateConfig(func(config *storage.Config) {
		config.PromptMinimumMegabytes = 1
	}); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte(
		`<html><head><meta name="ullage-token" content=""></head></html>`)}}
	local, err := server.New(manager, server.Options{Version: "e2e", Static: static})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = local.Serve() }()
	t.Cleanup(func() { _ = local.Close() })

	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	launch := local.NewLaunchURL()
	response, err := httpClient.Get(launch)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(response.Body)
	response.Body.Close()
	token := regexp.MustCompile(`content="([0-9a-f]+)"`).FindSubmatch(page)
	if token == nil {
		t.Fatalf("no token in the page: %s", page)
	}
	base := launch[:strings.Index(launch, "/launch")]
	return &client{t: t, http: httpClient, base: base, token: string(token[1]),
		origin: base}, root
}

func TestScanClassifyExplainAndRecycle(t *testing.T) {
	api, root := startServer(t)

	// Without the token header nothing is answered.
	bare := &client{t: t, http: api.http, base: api.base, origin: api.origin}
	if status := bare.call(http.MethodGet, "/api/results", nil, nil); status != 401 &&
		status != 403 {
		t.Fatalf("request without token: %d", status)
	}

	if status := api.call(http.MethodPost, "/api/scan", map[string]any{
		"roots": []string{root}}, nil); status >= 300 {
		t.Fatalf("start scan: %d", status)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		var progress struct{ State string }
		api.call(http.MethodGet, "/api/scan/progress", nil, &progress)
		if progress.State == "finished" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan state %q", progress.State)
		}
		time.Sleep(50 * time.Millisecond)
	}

	var exported struct {
		Code    string
		Batches []struct{ Text string }
	}
	api.call(http.MethodGet, "/api/export", nil, &exported)
	text := exported.Batches[0].Text
	idOf := func(fragment string) string {
		for _, match := range regexp.MustCompile(`(?m)^ (\d+) \S+ (\S+)`).FindAllStringSubmatch(
			text, -1) {
			if strings.Contains(match[2], fragment) {
				return match[1]
			}
		}
		t.Fatalf("%s not in the prompt:\n%s", fragment, text)
		return ""
	}
	code := exported.Code
	reply := fmt.Sprintf("@SESSION %s\ncat|conf|regen|del|ids\nJavaScript packages|high|yes|1|%s\n"+
		"Home videos|high|no|0|%s\n@END %s", code, idOf("node_modules"), idOf("Videos"), code)
	var pasted struct{ Imported bool }
	api.call(http.MethodPost, "/api/paste", map[string]any{"batch": 1, "text": reply}, &pasted)
	if !pasted.Imported {
		t.Fatal("round A not imported")
	}

	api.call(http.MethodPost, "/api/results/explain", map[string]any{"number": 1,
		"queued": true}, nil)
	var withRoundB struct{ Explanation struct{ Round int } }
	api.call(http.MethodGet, "/api/export", nil, &withRoundB)
	explanation := fmt.Sprintf("@SESSION %s\n#1\nwhat: npm packages.\nlost: Nothing.\n"+
		"step: Reinstall with npm install.\n@END %s", code, code)
	api.call(http.MethodPost, "/api/paste", map[string]any{
		"batch": withRoundB.Explanation.Round, "text": explanation}, &pasted)
	if !pasted.Imported {
		t.Fatal("round B not imported")
	}

	var results struct {
		Categories []struct {
			Key        string
			Selectable bool
			Items      []struct{ Key string }
		}
	}
	api.call(http.MethodGet, "/api/results", nil, &results)
	if len(results.Categories) != 1 || !results.Categories[0].Selectable {
		t.Fatalf("results = %+v", results)
	}
	category := results.Categories[0]
	key := category.Key + "|" + category.Items[0].Key

	var plan struct {
		ID    string
		Ready struct{ Count int }
	}
	api.call(http.MethodPost, "/api/delete/prepare", map[string]any{
		"keys": []string{key, "ai:2|" + category.Items[0].Key}}, &plan)
	if plan.Ready.Count != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	api.call(http.MethodPost, "/api/delete/execute", map[string]any{"planId": plan.ID}, nil)
	var run struct {
		Finished bool
		Removed  struct{ Count int }
		Items    []struct{ Status, Reason string }
	}
	for deadline := time.Now().Add(30 * time.Second); !run.Finished; {
		api.call(http.MethodGet, "/api/delete/progress", nil, &run)
		if time.Now().After(deadline) {
			t.Fatal("deletion did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if run.Removed.Count != 1 || run.Items[0].Status != "recycled" {
		t.Fatalf("run = %+v", run)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "app", "node_modules")); !os.IsNotExist(
		err) {
		t.Fatalf("node_modules still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Videos", "clip.mp4")); err != nil {
		t.Fatalf("a file the AI said to keep was touched: %v", err)
	}

	var history struct {
		Entries []struct{ Items, Batches int }
	}
	api.call(http.MethodGet, "/api/history", nil, &history)
	if len(history.Entries) != 1 || history.Entries[0].Items != 1 ||
		history.Entries[0].Batches != 1 {
		t.Fatalf("history = %+v", history)
	}
}
