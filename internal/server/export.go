package server

import (
	"errors"
	"net/http"

	"github.com/toldry/ullage/internal/session"
)

func writeExportError(writer http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNoExport) {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error(),
			"code": "no-export"})
		return
	}
	writeRedactionError(writer, err)
}

func (server *Server) handleExport(writer http.ResponseWriter, _ *http.Request) {
	view, err := server.manager.Export(false)
	if err != nil {
		writeExportError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) handleRebuildExport(writer http.ResponseWriter, _ *http.Request) {
	view, err := server.manager.Export(true)
	if err != nil {
		writeExportError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) handlePaste(writer http.ResponseWriter, request *http.Request) {
	var paste session.PasteRequest
	if err := decodeJSON(request, &paste); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	result, err := server.manager.Paste(paste)
	if err != nil {
		writeExportError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func writeResultsError(writer http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNoSuchCategory) {
		writeJSONError(writer, http.StatusNotFound, err.Error())
		return
	}
	writeJSONError(writer, http.StatusConflict, err.Error())
}

func (server *Server) handleResults(writer http.ResponseWriter, _ *http.Request) {
	view, err := server.manager.Results()
	if err != nil {
		writeResultsError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) handleExpand(writer http.ResponseWriter, request *http.Request) {
	var expand struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(request, &expand); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	view, err := server.manager.MarkExpanded(expand.Key)
	if err != nil {
		writeResultsError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (server *Server) handleUnchangedFilter(writer http.ResponseWriter,
	request *http.Request) {
	var filter struct {
		Days int `json:"days"`
	}
	if err := decodeJSON(request, &filter); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	view, err := server.manager.SetUnchangedDays(filter.Days)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}
