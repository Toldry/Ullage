package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/toldry/ullage/internal/session"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	manager, err := session.NewManager(session.NativeEnvironment(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{
		"index.html":    {Data: []byte(`<html><head>` + tokenPlaceholder + `</head></html>`)},
		"assets/app.js": {Data: []byte(`console.log(1)`)},
	}
	server, err := New(manager, Options{Version: "test", Static: static})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.listener.Close() })
	return server
}

type requestOptions struct {
	method   string
	path     string
	host     string
	origin   string
	cookie   string
	token    string
	body     string
	noCookie bool
}

func (server *Server) do(options requestOptions) *httptest.ResponseRecorder {
	if options.method == "" {
		options.method = http.MethodGet
	}
	request := httptest.NewRequest(options.method, options.path, strings.NewReader(options.body))
	request.Host = server.guard.expectedHost
	if options.host != "" {
		request.Host = options.host
	}
	if options.origin != "" {
		request.Header.Set("Origin", options.origin)
	}
	cookie := server.guard.sessionSecret
	if options.cookie != "" {
		cookie = options.cookie
	}
	if !options.noCookie {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	}
	if options.token != "" {
		request.Header.Set(tokenHeaderName, options.token)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestValidRequestsSucceed(t *testing.T) {
	server := newTestServer(t)
	status := server.do(requestOptions{path: "/api/status", token: server.guard.apiToken})
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d %s", status.Code, status.Body)
	}
	heartbeat := server.do(requestOptions{method: http.MethodPost, path: "/api/heartbeat",
		token: server.guard.apiToken, origin: server.guard.expectedOrigin})
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d %s", heartbeat.Code, heartbeat.Body)
	}
	index := server.do(requestOptions{path: "/"})
	if index.Code != http.StatusOK ||
		!strings.Contains(index.Body.String(), server.guard.apiToken) {
		t.Fatalf("index did not receive the API token")
	}
	if csp := index.Header().Get("Content-Security-Policy"); !strings.Contains(csp,
		"default-src 'self'") {
		t.Fatalf("missing CSP: %q", csp)
	}
}

func TestMissingOrWrongTokensAreRejected(t *testing.T) {
	server := newTestServer(t)
	cases := map[string]requestOptions{
		"no cookie, no header": {path: "/api/status", noCookie: true},
		"no header":            {path: "/api/status"},
		"wrong header":         {path: "/api/status", token: "0123"},
		"wrong cookie": {path: "/api/status", token: server.guard.apiToken,
			cookie: "forged"},
		"cookie as header":      {path: "/api/status", token: server.guard.sessionSecret},
		"stream without cookie": {path: "/api/scan/stream", noCookie: true},
	}
	for name, options := range cases {
		if response := server.do(options); response.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, response.Code)
		}
	}
	page := server.do(requestOptions{path: "/", noCookie: true})
	if page.Code != http.StatusForbidden || strings.Contains(page.Body.String(),
		server.guard.apiToken) || !strings.Contains(page.Body.String(), "can't open a session") {
		t.Errorf("page without cookie: %d", page.Code)
	}
	asset := server.do(requestOptions{path: "/assets/app.js", noCookie: true})
	if asset.Code != http.StatusForbidden {
		t.Errorf("asset without cookie: %d", asset.Code)
	}
}

func TestWrongHostIsRejected(t *testing.T) {
	server := newTestServer(t)
	for _, host := range []string{"localhost:" + strconv.Itoa(server.port),
		"attacker.example", "127.0.0.1", "127.0.0.1:1"} {
		response := server.do(requestOptions{path: "/api/status", host: host,
			token: server.guard.apiToken})
		if response.Code != http.StatusMisdirectedRequest {
			t.Errorf("host %q: status %d", host, response.Code)
		}
	}
}

func TestWrongOriginIsRejected(t *testing.T) {
	server := newTestServer(t)
	for _, origin := range []string{"http://attacker.example", "null",
		"http://localhost:" + strconv.Itoa(server.port), "https://127.0.0.1:" +
			strconv.Itoa(server.port)} {
		response := server.do(requestOptions{method: http.MethodPost, path: "/api/heartbeat",
			origin: origin, token: server.guard.apiToken})
		if response.Code != http.StatusForbidden {
			t.Errorf("origin %q: status %d", origin, response.Code)
		}
	}
	missing := server.do(requestOptions{method: http.MethodPost, path: "/api/heartbeat",
		token: server.guard.apiToken})
	if missing.Code != http.StatusForbidden {
		t.Errorf("POST without Origin: status %d", missing.Code)
	}
}

func TestStateChangingEndpointsRejectGetAndQueryTokens(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/api/scan", "/api/scan/cancel", "/api/config",
		"/api/quit", "/api/relaunch-elevated", "/api/redaction/override", "/api/rules/save",
		"/api/rules/delete", "/api/rules/reset", "/api/export/rebuild", "/api/paste",
		"/api/results/explain", "/api/results/expand", "/api/delete/prepare",
		"/api/delete/execute", "/api/delete/stop", "/api/history/clear"} {
		response := server.do(requestOptions{path: path, token: server.guard.apiToken})
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status %d", path, response.Code)
		}
	}
	queryOnly := server.do(requestOptions{method: http.MethodPost,
		path:   "/api/heartbeat?token=" + url.QueryEscape(server.guard.apiToken),
		origin: server.guard.expectedOrigin})
	if queryOnly.Code != http.StatusUnauthorized {
		t.Errorf("token in query string accepted: %d", queryOnly.Code)
	}
}

func TestLaunchTokenIsSingleUse(t *testing.T) {
	server := newTestServer(t)
	launch, _ := url.Parse(server.NewLaunchURL())
	if launch.Hostname() != "127.0.0.1" {
		t.Fatalf("launch URL host = %s", launch.Host)
	}
	first := server.do(requestOptions{path: launch.RequestURI(), noCookie: true})
	if first.Code != http.StatusSeeOther || first.Header().Get("Location") != "/" {
		t.Fatalf("first launch: %d", first.Code)
	}
	setCookie := first.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie,
		"SameSite=Strict") || !strings.Contains(setCookie, server.guard.sessionSecret) {
		t.Fatalf("cookie = %q", setCookie)
	}
	second := server.do(requestOptions{path: launch.RequestURI(), noCookie: true})
	if second.Code != http.StatusForbidden ||
		!strings.Contains(second.Body.String(), "already used") ||
		strings.Contains(second.Header().Get("Set-Cookie"), server.guard.sessionSecret) {
		t.Fatalf("second launch: %d %q", second.Code, second.Header().Get("Set-Cookie"))
	}
	forged := server.do(requestOptions{path: "/launch?t=deadbeef", noCookie: true})
	if forged.Code != http.StatusForbidden {
		t.Fatalf("forged launch: %d", forged.Code)
	}
}

func TestListensOnLoopbackOnly(t *testing.T) {
	server := newTestServer(t)
	address := server.listener.Addr().(*net.TCPAddr)
	if !address.IP.Equal(net.IPv4(127, 0, 0, 1)) || address.Port == 0 {
		t.Fatalf("listening on %v", address)
	}
	go server.Serve()
	defer server.Close()
	// A real round trip through the listener, without credentials, must be refused.
	response, err := http.Get("http://" + address.String() + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request: %d", response.StatusCode)
	}
}

func TestRulesResetNeedsExplicitConfirmation(t *testing.T) {
	server := newTestServer(t)
	for _, body := range []string{`{}`, `{"confirm":"yes"}`, `{"confirm":"RESET"}`} {
		response := server.do(requestOptions{method: http.MethodPost, path: "/api/rules/reset",
			token: server.guard.apiToken, origin: server.guard.expectedOrigin, body: body})
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %s: status %d", body, response.Code)
		}
	}
	summary := server.do(requestOptions{path: "/api/redaction/summary",
		token: server.guard.apiToken})
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(),
		`"available":true`) {
		t.Errorf("summary: %d %s", summary.Code, summary.Body)
	}
}
