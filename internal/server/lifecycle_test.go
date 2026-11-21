package server

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// quitTestServer counts OnQuit calls and lets the test set what is in progress.
type quitTestServer struct {
	*Server
	quits    atomic.Int32
	scanning atomic.Bool
	deleting atomic.Bool
}

func newQuitTestServer(t *testing.T) *quitTestServer {
	t.Helper()
	test := &quitTestServer{Server: newTestServer(t)}
	test.options.OnQuit = func() { test.quits.Add(1) }
	test.work = func() Work {
		return Work{Scanning: test.scanning.Load(), Deleting: test.deleting.Load()}
	}
	return test
}

func (test *quitTestServer) quit(t *testing.T, body string) (int, quitResponse) {
	t.Helper()
	response := test.do(requestOptions{method: http.MethodPost, path: "/api/quit", body: body,
		token: test.guard.apiToken, origin: test.guard.expectedOrigin})
	var decoded quitResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("quit response %q: %v", response.Body, err)
	}
	return response.Code, decoded
}

func (test *quitTestServer) waitForQuits(t *testing.T, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for test.quits.Load() < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := test.quits.Load(); got != want {
		t.Fatalf("OnQuit called %d times, want %d", got, want)
	}
}

func TestQuitWhileIdleExits(t *testing.T) {
	test := newQuitTestServer(t)
	code, response := test.quit(t, `{}`)
	if code != http.StatusOK || !response.OK {
		t.Fatalf("quit = %d %+v", code, response)
	}
	test.waitForQuits(t, 1)
}

func TestQuitDuringWorkNeedsConfirmation(t *testing.T) {
	for name, set := range map[string]func(*quitTestServer){
		"scan":     func(test *quitTestServer) { test.scanning.Store(true) },
		"deletion": func(test *quitTestServer) { test.deleting.Store(true) },
	} {
		t.Run(name, func(t *testing.T) {
			test := newQuitTestServer(t)
			set(test)
			code, response := test.quit(t, `{}`)
			if code != http.StatusConflict || response.OK || response.Work != test.work() {
				t.Fatalf("unconfirmed quit = %d %+v", code, response)
			}
			time.Sleep(700 * time.Millisecond)
			if test.quits.Load() != 0 {
				t.Fatal("an unconfirmed quit during work exited")
			}
		})
	}
}

func TestConfirmedQuitWaitsForTheDeletionToStop(t *testing.T) {
	previousPoll := deletionStopPoll
	deletionStopPoll = 5 * time.Millisecond
	t.Cleanup(func() { deletionStopPoll = previousPoll })
	test := newQuitTestServer(t)
	test.deleting.Store(true)
	code, response := test.quit(t, `{"confirm":true}`)
	if code != http.StatusOK || !response.OK || !response.Deleting {
		t.Fatalf("confirmed quit = %d %+v", code, response)
	}
	// The item being deleted is still in progress: the process must not exit yet.
	time.Sleep(800 * time.Millisecond)
	if test.quits.Load() != 0 {
		t.Fatal("exited while the deletion was still running")
	}
	test.deleting.Store(false)
	test.waitForQuits(t, 1)
}

func TestRepeatedQuitExitsOnce(t *testing.T) {
	test := newQuitTestServer(t)
	for range 3 {
		if code, _ := test.quit(t, `{}`); code != http.StatusOK {
			t.Fatalf("quit = %d", code)
		}
	}
	test.waitForQuits(t, 1)
	time.Sleep(700 * time.Millisecond)
	if got := test.quits.Load(); got != 1 {
		t.Fatalf("OnQuit called %d times", got)
	}
}

func TestQuitRejectsUnknownFields(t *testing.T) {
	test := newQuitTestServer(t)
	response := test.do(requestOptions{method: http.MethodPost, path: "/api/quit",
		body: `{"force":true}`, token: test.guard.apiToken, origin: test.guard.expectedOrigin})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("quit with unknown field = %d", response.Code)
	}
}

func TestIdleShutdown(t *testing.T) {
	const limit = 30 * time.Minute
	test := newQuitTestServer(t)
	if test.IdleShutdownDue(limit) {
		t.Fatal("due right after start")
	}
	test.lastActivity.Store(time.Now().Add(-29 * time.Minute).UnixNano())
	if test.IdleShutdownDue(limit) {
		t.Fatal("due before the limit")
	}
	test.lastActivity.Store(time.Now().Add(-31 * time.Minute).UnixNano())
	if !test.IdleShutdownDue(limit) {
		t.Fatal("not due after the limit with nothing running")
	}

	for name, flag := range map[string]*atomic.Bool{
		"scan": &test.scanning, "deletion": &test.deleting,
	} {
		test.lastActivity.Store(time.Now().Add(-2 * time.Hour).UnixNano())
		flag.Store(true)
		if test.IdleShutdownDue(limit) {
			t.Fatalf("due while a %s runs", name)
		}
		flag.Store(false)
		// The work just ended: the user gets the full limit to come back to its result.
		if test.IdleShutdownDue(limit) {
			t.Fatalf("due right after a %s ended", name)
		}
	}
}

func TestHeartbeatResetsTheIdleClock(t *testing.T) {
	test := newQuitTestServer(t)
	test.lastActivity.Store(time.Now().Add(-31 * time.Minute).UnixNano())
	response := test.do(requestOptions{method: http.MethodPost, path: "/api/heartbeat",
		token: test.guard.apiToken, origin: test.guard.expectedOrigin})
	if response.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d", response.Code)
	}
	if test.IdleShutdownDue(30 * time.Minute) {
		t.Fatal("due right after a heartbeat")
	}
}

func TestUnauthenticatedRequestsDoNotKeepTheProcessAlive(t *testing.T) {
	test := newQuitTestServer(t)
	test.lastActivity.Store(time.Now().Add(-31 * time.Minute).UnixNano())
	test.do(requestOptions{method: http.MethodPost, path: "/api/heartbeat", noCookie: true})
	if !test.IdleShutdownDue(30 * time.Minute) {
		t.Fatal("a rejected request reset the idle clock")
	}
}
