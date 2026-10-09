package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/session"
)

// writeRedactionError maps session errors to statuses the UI can act on.
func writeRedactionError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrRulesUnavailable):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error(),
			"code": "rules-unavailable"})
	case errors.Is(err, session.ErrStaleScan):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error(),
			"code": "stale-scan"})
	default:
		writeJSONError(writer, http.StatusBadRequest, err.Error())
	}
}

func nodeParameter(request *http.Request, name string) (scanner.NodeIndex, error) {
	value, err := strconv.ParseInt(request.URL.Query().Get(name), 10, 32)
	if err != nil {
		return scanner.NoNode, errors.New(name + " must be an integer")
	}
	return scanner.NodeIndex(value), nil
}

func (server *Server) handleRedactionSummary(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.manager.RedactionSummary())
}

func (server *Server) handleRedactionChildren(writer http.ResponseWriter,
	request *http.Request) {
	parent, err := nodeParameter(request, "parent")
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	children, err := server.manager.Children(request.URL.Query().Get("code"), parent)
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	// Rendering may allocate placeholder numbers; keep them stable for later sessions.
	_ = server.manager.PersistPlaceholders()
	writeJSON(writer, http.StatusOK, children)
}

func (server *Server) handleRedactionItem(writer http.ResponseWriter, request *http.Request) {
	index, err := nodeParameter(request, "id")
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := server.manager.Detail(request.URL.Query().Get("code"), index)
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	_ = server.manager.PersistPlaceholders()
	writeJSON(writer, http.StatusOK, detail)
}

func (server *Server) handleRedactionOverride(writer http.ResponseWriter,
	request *http.Request) {
	var override session.OverrideRequest
	if err := decodeJSON(request, &override); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := server.manager.SetOverride(override); err != nil {
		writeRedactionError(writer, err)
		return
	}
	detail, err := server.manager.Detail(override.SessionCode, override.ID)
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func (server *Server) handleRules(writer http.ResponseWriter, _ *http.Request) {
	views, err := server.manager.Rules()
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, views)
}

func (server *Server) handleSaveRule(writer http.ResponseWriter, request *http.Request) {
	var ruleRequest session.RuleRequest
	if err := decodeJSON(request, &ruleRequest); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	rule, err := server.manager.SaveRule(ruleRequest)
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, rule)
}

type ruleIdentifier struct {
	ID string `json:"id"`
}

func (server *Server) handleDeleteRule(writer http.ResponseWriter, request *http.Request) {
	var identifier ruleIdentifier
	if err := decodeJSON(request, &identifier); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := server.manager.DeleteRule(identifier.ID); err != nil {
		writeRedactionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"ok": true})
}

type resetConfirmation struct {
	Confirm string `json:"confirm"`
}

// handleResetRules requires the literal confirmation "reset" so a stray click or script
// cannot discard the user's rules.
func (server *Server) handleResetRules(writer http.ResponseWriter, request *http.Request) {
	var confirmation resetConfirmation
	if err := decodeJSON(request, &confirmation); err != nil || confirmation.Confirm !=
		"reset" {
		writeJSONError(writer, http.StatusBadRequest, `send {"confirm":"reset"} to proceed`)
		return
	}
	aside, err := server.manager.ResetUnreadableRules()
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"movedTo": aside})
}

func (server *Server) handleRuleHits(writer http.ResponseWriter, request *http.Request) {
	hits, err := server.manager.RuleHits(request.URL.Query().Get("id"))
	if errors.Is(err, session.ErrNoSuchRule) {
		writeJSONError(writer, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeRedactionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, hits)
}
