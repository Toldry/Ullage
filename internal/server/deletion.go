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
		Keys []string `json:"keys"`
	}
	if err := decodeJSON(request, &selection); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	plan, err := server.manager.PrepareDeletion(selection.Keys)
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
