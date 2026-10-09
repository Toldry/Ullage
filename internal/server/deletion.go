package server

import (
	"errors"
	"net/http"

	"github.com/toldry/ullage/internal/session"
)

func writeDeletionError(writer http.ResponseWriter, err error) {
	status := http.StatusConflict
	if errors.Is(err, session.ErrConfirmation) {
		status = http.StatusBadRequest
	}
	writeJSONError(writer, status, err.Error())
}

func (server *Server) handlePrepareDeletion(writer http.ResponseWriter,
	request *http.Request) {
	var selection struct {
		Keys   []string `json:"keys"`
		Rescan bool     `json:"rescan"`
	}
	if err := decodeJSON(request, &selection); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	prepare := server.manager.PrepareDeletion
	if selection.Rescan {
		prepare = server.manager.PrepareRescan
	}
	plan, err := prepare(selection.Keys)
	if err != nil {
		writeDeletionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, plan)
}

func (server *Server) handleExecuteDeletion(writer http.ResponseWriter,
	request *http.Request) {
	var execute session.ExecuteRequest
	if err := decodeJSON(request, &execute); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	run, err := server.manager.ExecuteDeletion(execute)
	if err != nil {
		writeDeletionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, run)
}

func (server *Server) handleDeletionProgress(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.manager.DeletionProgress())
}

func (server *Server) handleStopDeletion(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.manager.StopDeletion())
}

func (server *Server) handleHistory(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.manager.History())
}

func (server *Server) handleClearHistory(writer http.ResponseWriter, request *http.Request) {
	var clear struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(request, &clear); err != nil || !clear.Confirm {
		writeJSONError(writer, http.StatusBadRequest, "clearing history needs confirm: true")
		return
	}
	view, err := server.manager.ClearHistory()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) handleDeletionPlan(writer http.ResponseWriter, _ *http.Request) {
	plan, err := server.manager.DeletionPlanProgress()
	if err != nil {
		writeJSONError(writer, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, plan)
}
