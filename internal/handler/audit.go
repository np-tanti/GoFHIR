package handler

import (
	"net/http"
	"strconv"

	"github.com/graphic/gofhir/internal/auditor"
)

type AuditHandler struct {
	store *auditor.Store
}

func NewAudit(store *auditor.Store) *AuditHandler {
	return &AuditHandler{store: store}
}

type auditEntryResponse struct {
	Seq       uint64 `json:"seq"`
	Timestamp int64  `json:"timestamp"`
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	SessionID string `json:"session_id,omitempty"`
}

func (h *AuditHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sinceStr := q.Get("since")
	limitStr := q.Get("limit")

	var seqFrom uint64 = 1
	if sinceStr != "" {
		if v, err := strconv.ParseUint(sinceStr, 10, 64); err == nil {
			seqFrom = v
		}
	}
	limit := 100
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	seqTo := seqFrom + uint64(limit) - 1
	entries, err := h.store.ReadRange(r.Context(), seqFrom, seqTo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := make([]auditEntryResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, auditEntryResponse{
			Seq:       e.Seq,
			Timestamp: e.Timestamp,
			Action:    e.Action,
			ActorID:   e.ActorID,
			SessionID: e.SessionID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": resp,
		"total":   len(resp),
	})
}

func (h *AuditHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /audit/entries", h.ListEntries)
}