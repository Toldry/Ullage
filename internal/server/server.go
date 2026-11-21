// Package server serves the UI and JSON API on 127.0.0.1 only. Every request passes the
// Guard; this package contains no scanning logic of its own.
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/toldry/ullage/internal/overview"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/session"
	"github.com/toldry/ullage/internal/storage"
)

const (
	maximumRequestBytes = 64 << 10
	streamInterval      = 300 * time.Millisecond
	// The page puts the API token here; it is only rendered for requests that already
	// carry the session cookie.
	tokenPlaceholder = `<meta name="ullage-token" content="">`
)

// contentSecurityPolicy forbids every remote resource, so even a regression that adds a
// CDN link cannot make the browser fetch it.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Options configures a Server.
type Options struct {
	Version string
	// Static is the built frontend. Tests pass an in-memory file system.
	Static fs.FS
	// RelaunchElevated restarts the app through UAC; nil when unsupported.
	RelaunchElevated func() error
	// OnQuit is called when the user quits from the UI or relaunches elevated.
	OnQuit func()
}

// Server is the local HTTP server.
type Server struct {
	options      Options
	guard        *Guard
	manager      *session.Manager
	listener     net.Listener
	httpServer   *http.Server
	port         int
	lastActivity atomic.Int64
}

// New listens on a random free port of 127.0.0.1. It never binds other interfaces.
func New(manager *session.Manager, options Options) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &Server{options: options, guard: NewGuard(port), manager: manager,
		listener: listener, port: port}
	server.httpServer = &http.Server{
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}
	server.touch()
	return server, nil
}

// Port returns the listening port.
func (server *Server) Port() int { return server.port }

// NewLaunchURL returns a fresh single-use link to open in the browser.
func (server *Server) NewLaunchURL() string { return server.guard.NewLaunchURL() }

// Serve blocks until the server is closed.
func (server *Server) Serve() error {
	err := server.httpServer.Serve(server.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the server.
func (server *Server) Close() error { return server.httpServer.Close() }

// IdleFor reports how long no authenticated request arrived.
func (server *Server) IdleFor() time.Duration {
	return time.Since(time.Unix(0, server.lastActivity.Load()))
}

func (server *Server) touch() { server.lastActivity.Store(time.Now().UnixNano()) }

type route struct {
	method  string
	handler func(http.ResponseWriter, *http.Request)
	// maximumBodyBytes overrides the default request size limit.
	maximumBodyBytes int64
}

// maximumPasteBytes allows a reply for a full batch with generous slack.
const maximumPasteBytes = 4 << 20

func (server *Server) apiRoutes() map[string]route {
	return map[string]route{
		"/api/status":             {method: http.MethodGet, handler: server.handleStatus},
		"/api/volumes":            {method: http.MethodGet, handler: server.handleVolumes},
		"/api/scan/progress":      {method: http.MethodGet, handler: server.handleProgress},
		"/api/overview":           {method: http.MethodGet, handler: server.handleOverview},
		"/api/scan":               {method: http.MethodPost, handler: server.handleStartScan},
		"/api/scan/pause":         {method: http.MethodPost, handler: server.handleScanControl},
		"/api/scan/resume":        {method: http.MethodPost, handler: server.handleScanControl},
		"/api/scan/cancel":        {method: http.MethodPost, handler: server.handleScanControl},
		"/api/config":             {method: http.MethodPost, handler: server.handleConfig},
		"/api/heartbeat":          {method: http.MethodPost, handler: server.handleHeartbeat},
		"/api/relaunch-elevated":  {method: http.MethodPost, handler: server.handleRelaunch},
		"/api/quit":               {method: http.MethodPost, handler: server.handleQuit},
		"/api/redaction/summary":  {method: http.MethodGet, handler: server.handleRedactionSummary},
		"/api/redaction/children": {method: http.MethodGet, handler: server.handleRedactionChildren},
		"/api/redaction/item":     {method: http.MethodGet, handler: server.handleRedactionItem},
		"/api/redaction/override": {method: http.MethodPost, handler: server.handleRedactionOverride},
		"/api/rules":              {method: http.MethodGet, handler: server.handleRules},
		"/api/rules/hits":         {method: http.MethodGet, handler: server.handleRuleHits},
		"/api/rules/save":         {method: http.MethodPost, handler: server.handleSaveRule},
		"/api/rules/delete":       {method: http.MethodPost, handler: server.handleDeleteRule},
		"/api/rules/reset":        {method: http.MethodPost, handler: server.handleResetRules},
		"/api/export":             {method: http.MethodGet, handler: server.handleExport},
		"/api/export/rebuild":     {method: http.MethodPost, handler: server.handleRebuildExport},
		"/api/paste": {method: http.MethodPost, handler: server.handlePaste,
			maximumBodyBytes: maximumPasteBytes},
		"/api/results":        {method: http.MethodGet, handler: server.handleResults},
		"/api/results/expand": {method: http.MethodPost, handler: server.handleExpand},
		"/api/results/override-keep": {method: http.MethodPost,
			handler: server.handleOverrideKeep},
		"/api/results/unchanged": {method: http.MethodPost,
			handler: server.handleUnchangedFilter},
		"/api/delete/prepare": {method: http.MethodPost, handler: server.handlePrepareDeletion,
			maximumBodyBytes: maximumPasteBytes},
		"/api/delete/execute":  {method: http.MethodPost, handler: server.handleExecuteDeletion},
		"/api/delete/progress": {method: http.MethodGet, handler: server.handleDeletionProgress},
		"/api/delete/plan":     {method: http.MethodGet, handler: server.handleDeletionPlan},
		"/api/delete/stop":     {method: http.MethodPost, handler: server.handleStopDeletion},
		"/api/history":         {method: http.MethodGet, handler: server.handleHistory},
		"/api/history/clear":   {method: http.MethodPost, handler: server.handleClearHistory},
	}
}

// ServeHTTP applies the guard before any routing.
func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(writer)
	if status, reason := server.guard.checkTransport(request); status != 0 {
		http.Error(writer, reason, status)
		return
	}

	switch {
	case request.URL.Path == "/launch":
		server.handleLaunch(writer, request)
	case request.URL.Path == "/api/scan/stream":
		if !server.requireSession(writer, request, false) {
			return
		}
		server.handleStream(writer, request)
	case strings.HasPrefix(request.URL.Path, "/api/"):
		server.handleAPI(writer, request)
	default:
		if !server.guard.hasSessionCookie(request) {
			server.writeInvalidLinkPage(writer, "no session for this tab")
			return
		}
		server.handleStatic(writer, request)
	}
}

func setSecurityHeaders(writer http.ResponseWriter) {
	headers := writer.Header()
	headers.Set("Content-Security-Policy", contentSecurityPolicy)
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("Cache-Control", "no-store")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("Cross-Origin-Opener-Policy", "same-origin")
	headers.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// requireSession checks the cookie and, for API calls, the header token. EventSource
// cannot send headers, so the progress stream is the one endpoint with cookie only; it is
// read-only.
func (server *Server) requireSession(writer http.ResponseWriter, request *http.Request,
	requireHeader bool) bool {
	if !server.guard.hasSessionCookie(request) ||
		(requireHeader && !server.guard.hasAPIToken(request)) {
		writeJSONError(writer, http.StatusUnauthorized, "missing or invalid session token")
		return false
	}
	if request.Method != http.MethodGet && request.Header.Get("Origin") == "" {
		// Browsers always send Origin on POST; its absence means a non-browser client
		// or a stripped request, neither of which we serve.
		writeJSONError(writer, http.StatusForbidden, "origin required")
		return false
	}
	server.touch()
	return true
}

func (server *Server) handleAPI(writer http.ResponseWriter, request *http.Request) {
	selected, known := server.apiRoutes()[request.URL.Path]
	if !known {
		writeJSONError(writer, http.StatusNotFound, "unknown endpoint")
		return
	}
	if request.Method != selected.method {
		writer.Header().Set("Allow", selected.method)
		writeJSONError(writer, http.StatusMethodNotAllowed, "use "+selected.method)
		return
	}
	if !server.requireSession(writer, request, true) {
		return
	}
	limit := int64(maximumRequestBytes)
	if selected.maximumBodyBytes > 0 {
		limit = selected.maximumBodyBytes
	}
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	selected.handler(writer, request)
}

func (server *Server) handleLaunch(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := server.guard.redeemLaunchToken(request.URL.Query().Get("t"))
	if !result.accepted {
		server.writeInvalidLinkPage(writer, "token rejected: "+result.reason)
		return
	}
	http.SetCookie(writer, server.guard.sessionCookie())
	// Redirecting drops the token from the address bar and browser history.
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func (server *Server) handleStatic(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	server.touch()
	name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if name == "" || !strings.Contains(path.Base(name), ".") {
		server.serveIndex(writer)
		return
	}
	content, err := fs.ReadFile(server.options.Static, name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", contentTypeFor(name))
	_, _ = writer.Write(content)
}

func (server *Server) serveIndex(writer http.ResponseWriter) {
	content, err := fs.ReadFile(server.options.Static, "index.html")
	if err != nil {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, "The UI was not built into this binary. "+
			"Run `npm ci && npm run build` in web/ and rebuild.")
		return
	}
	withToken := bytes.Replace(content, []byte(tokenPlaceholder),
		[]byte(`<meta name="ullage-token" content="`+server.guard.apiToken+`">`), 1)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(withToken)
}

func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeJSONError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

type statusResponse struct {
	Version     string         `json:"version"`
	Platform    string         `json:"platform"`
	Elevated    bool           `json:"elevated"`
	Port        int            `json:"port"`
	State       session.State  `json:"state"`
	SessionCode string         `json:"sessionCode"`
	HasOverview bool           `json:"hasOverview"`
	Config      storage.Config `json:"config"`
	CanRelaunch bool           `json:"canRelaunch"`
	FirstRun    bool           `json:"firstRun"`
}

func (server *Server) handleStatus(writer http.ResponseWriter, _ *http.Request) {
	progress := server.manager.Progress()
	completed := server.manager.Completed()
	code := progress.SessionCode
	if completed != nil && progress.State != session.StateRunning {
		code = completed.Code
	}
	writeJSON(writer, http.StatusOK, statusResponse{
		Version:     server.options.Version,
		Platform:    platform.Name,
		Elevated:    server.manager.Elevated(),
		Port:        server.port,
		State:       progress.State,
		SessionCode: code,
		HasOverview: completed != nil,
		Config:      server.manager.Config(),
		CanRelaunch: server.options.RelaunchElevated != nil && !server.manager.Elevated(),
		FirstRun:    server.manager.FirstRun(),
	})
}

type volumeResponse struct {
	Root       string `json:"root"`
	Name       string `json:"name"`
	Label      string `json:"label"`
	FileSystem string `json:"fileSystem"`
	Kind       string `json:"kind"`
	TotalBytes int64  `json:"totalBytes"`
	FreeBytes  int64  `json:"freeBytes"`
	UsedBytes  int64  `json:"usedBytes"`
}

func (server *Server) handleVolumes(writer http.ResponseWriter, _ *http.Request) {
	volumes, err := server.manager.Volumes()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	response := []volumeResponse{}
	for _, volume := range volumes {
		response = append(response, volumeResponse{Root: volume.RootPath,
			Name: volume.DisplayName, Label: volume.Label, FileSystem: volume.FileSystem,
			Kind: volume.Kind, TotalBytes: volume.TotalBytes, FreeBytes: volume.FreeBytes,
			UsedBytes: volume.UsedBytes()})
	}
	writeJSON(writer, http.StatusOK, response)
}

type progressResponse struct {
	session.Progress
	Log []session.LogLine `json:"log"`
}

func (server *Server) handleProgress(writer http.ResponseWriter, request *http.Request) {
	after, _ := strconv.ParseInt(request.URL.Query().Get("after"), 10, 64)
	writeJSON(writer, http.StatusOK, progressResponse{Progress: server.manager.Progress(),
		Log: server.manager.LogSince(after)})
}

// handleStream pushes progress as Server-Sent Events until the client disconnects.
func (server *Server) handleStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSONError(writer, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Connection", "keep-alive")
	ticker := time.NewTicker(streamInterval)
	defer ticker.Stop()
	var sentSequence int64
	for {
		progress := server.manager.Progress()
		lines := server.manager.LogSince(sentSequence)
		if len(lines) > 0 {
			sentSequence = lines[len(lines)-1].Sequence
		}
		payload, _ := json.Marshal(progressResponse{Progress: progress, Log: lines})
		if _, err := io.WriteString(writer, "event: progress\ndata: "+string(payload)+
			"\n\n"); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

type overviewResponse struct {
	SessionCode string `json:"sessionCode"`
	overview.Model
}

func (server *Server) handleOverview(writer http.ResponseWriter, _ *http.Request) {
	completed := server.manager.Completed()
	if completed == nil {
		writeJSONError(writer, http.StatusNotFound, "no finished scan yet")
		return
	}
	writeJSON(writer, http.StatusOK, overviewResponse{SessionCode: completed.Code,
		Model: completed.Overview})
}

func (server *Server) handleStartScan(writer http.ResponseWriter, request *http.Request) {
	var scanRequest session.ScanRequest
	if err := decodeJSON(request, &scanRequest); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := server.manager.StartScan(scanRequest); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, session.ErrScanRunning) {
			status = http.StatusConflict
		}
		writeJSONError(writer, status, err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, server.manager.Progress())
}

func (server *Server) handleScanControl(writer http.ResponseWriter, request *http.Request) {
	switch path.Base(request.URL.Path) {
	case "pause":
		server.manager.Pause()
	case "resume":
		server.manager.Resume()
	case "cancel":
		server.manager.Cancel()
	}
	writeJSON(writer, http.StatusOK, server.manager.Progress())
}

type configRequest struct {
	Theme *string `json:"theme"`
}

func (server *Server) handleConfig(writer http.ResponseWriter, request *http.Request) {
	var update configRequest
	if err := decodeJSON(request, &update); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if update.Theme != nil && *update.Theme != "dark" && *update.Theme != "light" &&
		*update.Theme != "" {
		writeJSONError(writer, http.StatusBadRequest, "theme must be dark or light")
		return
	}
	config, err := server.manager.UpdateConfig(func(config *storage.Config) {
		if update.Theme != nil {
			config.Theme = *update.Theme
		}
	})
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, config)
}

func (server *Server) handleHeartbeat(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) handleRelaunch(writer http.ResponseWriter, _ *http.Request) {
	if server.options.RelaunchElevated == nil {
		writeJSONError(writer, http.StatusNotImplemented, "not supported on this platform")
		return
	}
	if err := server.options.RelaunchElevated(); err != nil {
		// Declining the UAC prompt lands here; the current window keeps working.
		writeJSONError(writer, http.StatusConflict, "relaunch did not start: "+err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	server.quitSoon()
}

func (server *Server) handleQuit(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
	server.quitSoon()
}

func (server *Server) quitSoon() {
	if server.options.OnQuit != nil {
		// Let the response reach the browser before the listener closes.
		time.AfterFunc(500*time.Millisecond, server.options.OnQuit)
	}
}

func (server *Server) writeInvalidLinkPage(writer http.ResponseWriter, reason string) {
	// This page is self-contained so it needs no authenticated asset requests, and it
	// never includes scan data.
	writer.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusForbidden)
	detail := html.EscapeString(server.guard.expectedHost + " · " + reason)
	_, _ = io.WriteString(writer, strings.Replace(invalidLinkPage, "{{DETAIL}}", detail, 1))
}

const invalidLinkPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Ullage · link not valid</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root{--bg:#fafaf8;--fg:#16171a;--fg2:#4f535a;--fg3:#6c7077;--line2:#cdcdc6;--bg3:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#101114;--fg:#e7e8ea;--fg2:#a9adb4;
--fg3:#80858e;--line2:#30343b;--bg3:#16181c}}
html,body{margin:0;height:100%;background:var(--bg);color:var(--fg);
font:13px/1.45 system-ui,sans-serif}
main{min-height:100%;display:flex;align-items:center;justify-content:center;padding:40px;
box-sizing:border-box}
.box{width:100%;max-width:540px;display:flex;flex-direction:column;gap:22px}
h1{margin:0;font-size:24px;font-weight:600}p{margin:0;color:var(--fg2)}
.card{padding:16px 18px;border:1px solid var(--line2);border-radius:6px;background:var(--bg3)}
.card b{display:block;font-size:15px;margin-bottom:6px}
.mono{font-family:ui-monospace,Consolas,monospace;font-size:12px;color:var(--fg3)}
</style></head><body><main><div class="box">
<div><b>Ullage</b></div>
<div><h1>This link can't open a session</h1>
<p>Ullage only accepts the one-time link it opens when you launch it. This one was already
used, has expired, or was typed or bookmarked by hand.</p></div>
<div class="card"><b>Relaunch Ullage from the Start menu.</b>
<p>It opens a new tab with a fresh link. You can close this one.</p></div>
<div class="mono"><div>{{DETAIL}}</div><div>No scan data was loaded into this page.</div></div>
<p class="mono">Why the token: it stops other programs and websites on this PC from reading
your scan through the browser.</p>
</div></main></body></html>`
