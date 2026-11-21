package server

import (
	"net/http"
	"time"

	"github.com/toldry/ullage/internal/session"
)

// Shutting down from the UI and after inactivity (decision D-079).

// Work is what the process is doing that a shutdown would interrupt.
type Work struct {
	Scanning bool `json:"scanning"`
	Deleting bool `json:"deleting"`
}

// Busy reports whether anything is in progress.
func (work Work) Busy() bool { return work.Scanning || work.Deleting }

func (server *Server) managerWork() Work {
	return Work{
		Scanning: server.manager.Progress().State == session.StateRunning,
		Deleting: server.manager.DeletionRunning(),
	}
}

// deletionStopLimit bounds how long a quit waits for the item being deleted to finish.
// Stopping a deletion takes effect after the current item, and moving one large folder
// to the Recycle Bin can take a while; cutting it off mid-item is what the wait avoids.
const deletionStopLimit = 5 * time.Minute

// deletionStopPoll is how often a quit checks whether the deletion has stopped.
var deletionStopPoll = 100 * time.Millisecond

type quitResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Work
}

// handleQuit shuts the process down. While a scan or a deletion runs it answers 409 with
// what is running, and the UI asks the user before sending the request again with
// "confirm": true. A confirmed quit cancels the scan, stops the deletion after its current
// item and waits for it, so the history records what was deleted, then exits.
func (server *Server) handleQuit(writer http.ResponseWriter, request *http.Request) {
	var quit struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(request, &quit); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	work := server.work()
	if work.Busy() && !quit.Confirm {
		writeJSON(writer, http.StatusConflict, quitResponse{Work: work,
			Error: "a scan or deletion is in progress; confirm to stop it and quit"})
		return
	}
	writeJSON(writer, http.StatusOK, quitResponse{OK: true, Work: work})
	if !server.quitting.CompareAndSwap(false, true) {
		return
	}
	server.manager.Log("quit", "quit from the UI")
	if !work.Busy() {
		server.quitSoon()
		return
	}
	go server.stopWorkAndQuit(deletionStopLimit)
}

func (server *Server) stopWorkAndQuit(limit time.Duration) {
	server.manager.Cancel()
	server.manager.StopDeletion()
	deadline := time.Now().Add(limit)
	for server.work().Deleting && time.Now().Before(deadline) {
		time.Sleep(deletionStopPoll)
	}
	server.quitSoon()
}

// IdleShutdownDue reports whether the process should exit because no tab has been in
// touch for longer than limit. Work in progress counts as activity: it never lets the
// process exit, and the limit starts again once the work ends, so a scan that finishes
// long after its tab closed is still there when the user comes back within the limit.
func (server *Server) IdleShutdownDue(limit time.Duration) bool {
	if server.work().Busy() {
		server.touch()
		return false
	}
	return server.IdleFor() > limit
}
