package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/graphic/gofhir/internal/config"
	"github.com/graphic/gofhir/internal/fhir/storage"
)

type FHIRHandler struct {
	store *storage.Store
	cfg   *config.Config
}

func NewFHIR(store *storage.Store, cfg *config.Config) *FHIRHandler {
	return &FHIRHandler{store: store, cfg: cfg}
}

type operationOutcome struct {
	ResourceType string `json:"resourceType"`
	Issue        []struct {
		Severity    string `json:"severity"`
		Code        string `json:"code"`
		Diagnostics string `json:"diagnostics,omitempty"`
	} `json:"issue"`
}

func newOO(severity, code, diag string) operationOutcome {
	oo := operationOutcome{ResourceType: "OperationOutcome"}
	oo.Issue = append(oo.Issue, struct {
		Severity    string `json:"severity"`
		Code        string `json:"code"`
		Diagnostics string `json:"diagnostics,omitempty"`
	}{Severity: severity, Code: code, Diagnostics: diag})
	return oo
}

func errorsIsUnique(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "constraint failed")
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func (h *FHIRHandler) CapabilityStatement(w http.ResponseWriter, r *http.Request) {
	stmt := map[string]any{
		"resourceType": "CapabilityStatement",
		"status":        "draft",
		"date":          time.Now().UTC().Format(FHIR_DATE_FMT),
		"kind":          "instance",
		"software":      map[string]string{"name": "gofhir", "version": "0.1.0"},
		"implementation": map[string]string{"description": "FHIR R4 API"},
		"fhirVersion":   "4.0.1",
		"format":        []string{"application/fhir+json"},
		"rest": []map[string]any{
			{
				"mode": "server",
				"resource": []map[string]any{
					{
						"type": "Patient",
						"interaction": []map[string]string{{"code": "create"}, {"code": "read"}, {"code": "update"}, {"code": "delete"}, {"code": "search-type"}},
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(stmt)
}

func (h *FHIRHandler) Create(w http.ResponseWriter, r *http.Request) {
	resourceType := h.resolveResourceType(r)
	if resourceType == "" {
		resourceType = "patient"
	}
	data, err := readBody(r)
	if err != nil {
		h.writeOO(w, r, newOO("error", "required", "read body failed"), http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		h.writeOO(w, r, newOO("error", "required", "empty body"), http.StatusBadRequest)
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		h.writeOO(w, r, newOO("error", "invalid", "invalid json"), http.StatusBadRequest)
		return
	}
	if _, ok := obj["resourceType"]; !ok {
		h.writeOO(w, r, newOO("error", "required", "missing resourceType"), http.StatusBadRequest)
		return
	}
	patched := data
	if rt, ok := obj["resourceType"].(string); ok && rt != "" && rt != resourceType {
		obj["resourceType"] = resourceType
		patched, _ = json.Marshal(obj)
	}
	userID, _ := obj["id"].(string)
	if userID == "" {
		h.writeOO(w, r, newOO("error", "required", "id is required"), http.StatusBadRequest)
		return
	}
	_ = h.store.WithinTx(r.Context(), func(ctx context.Context) error {
		rec := &storage.Resource{ID: userID, Data: patched}
		created, err := h.store.Create(ctx, rec)
		if err != nil {
			if errorsIsUnique(err) {
				h.writeOO(w, r, newOO("error", "conflict", fmt.Sprintf("resource %q already exists", userID)), http.StatusConflict)
				return nil
			}
			h.error(w, r, http.StatusInternalServerError, "create", err)
			return nil
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		w.Header().Set("Location", fmt.Sprintf("/fhir/%s/%s", created.ResourceType, created.ID))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(struct {
			ResourceType string `json:"resourceType"`
			ID           string `json:"id"`
			Status       string `json:"status"`
		}{ResourceType: created.ResourceType, ID: created.ID, Status: "created"})
		return nil
	})
}

func (h *FHIRHandler) readVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	versionRaw := r.PathValue("version")
	version, err := strconv.Atoi(versionRaw)
	if err != nil {
		h.writeOO(w, r, newOO("error", "invalid", "version must be int"), http.StatusBadRequest)
		return
	}
	rec, err := h.store.ReadVersion(r.Context(), id, version)
	if err != nil {
		h.writeOO(w, r, newOO("error", "not-found", err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(json.RawMessage(rec.Data))
}

func (h *FHIRHandler) readLatest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := h.store.Read(r.Context(), id)
	if err != nil {
		h.writeOO(w, r, newOO("error", "not-found", err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(json.RawMessage(rec.Data))
}

func (h *FHIRHandler) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	resourceType := r.PathValue("type")
	data, err := readBody(r)
	if err != nil {
		h.error(w, r, http.StatusBadRequest, "read body", err)
		return
	}
	if len(data) == 0 {
		h.writeOO(w, r, newOO("error", "required", "empty body"), http.StatusBadRequest)
		return
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		h.writeOO(w, r, newOO("error", "invalid", "invalid json"), http.StatusBadRequest)
		return
	}
	if rt, ok := obj["resourceType"].(string); ok && rt != "" && rt != resourceType {
		obj["resourceType"] = resourceType
		data, _ = json.Marshal(obj)
	}
	rec := &storage.Resource{ID: id, Data: data}
	updated, err := h.store.Update(r.Context(), rec)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no rows") {
			h.writeOO(w, r, newOO("error", "not-found", err.Error()), http.StatusNotFound)
			return
		}
		h.error(w, r, http.StatusInternalServerError, "update", err)
		return
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	w.Header().Set("Location", fmt.Sprintf("/fhir/%s/%s/_history/%d", updated.ResourceType, updated.ID, updated.Version))
	_ = json.NewEncoder(w).Encode(struct {
		ResourceType string `json:"resourceType"`
		ID           string `json:"id"`
		Version      int    `json:"version"`
	}{ResourceType: updated.ResourceType, ID: updated.ID, Version: updated.Version})
}

func (h *FHIRHandler) softDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.SoftDelete(r.Context(), id); err != nil {
		h.writeOO(w, r, newOO("error", "not-found", err.Error()), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FHIRHandler) historyForResource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	history, err := h.store.History(r.Context(), id)
	if err != nil {
		h.writeOO(w, r, newOO("error", "exception", err.Error()), http.StatusInternalServerError)
		return
	}
	bundle := map[string]any{
		"resourceType": "Bundle",
		"type":          "history",
		"total":         len(history),
		"entry":         make([]map[string]any, 0, len(history)),
	}
	for _, rec := range history {
		bundle["entry"] = append(bundle["entry"].([]map[string]any), map[string]any{
			"fullUrl":  fmt.Sprintf("/fhir/%s/%s/_history/%d", rec.ResourceType, rec.ID, rec.Version),
			"request":  map[string]string{"method": "GET", "url": fmt.Sprintf("/fhir/%s/%s/_history/%d", rec.ResourceType, rec.ID, rec.Version)},
			"response": map[string]string{"status": "200 OK"},
			"resource": json.RawMessage(rec.Data),
		})
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(bundle)
}

func (h *FHIRHandler) searchType(w http.ResponseWriter, r *http.Request) {
	resourceType := r.PathValue("type")
	if resourceType == "" {
		resourceType = strings.TrimPrefix(r.URL.Path, "/fhir/")
		resourceType = strings.TrimSuffix(resourceType, "/")
		resourceType = strings.ToLower(resourceType)
	}
	filters := h.parseFilters(r)
	filters.MaxCount = h.cfg.SearchMaxCount
	res, err := h.store.Search(r.Context(), resourceType, filters)
	if err != nil {
		h.writeOO(w, r, newOO("error", "exception", err.Error()), http.StatusInternalServerError)
		return
	}
	bundle := map[string]any{
		"resourceType": "Bundle",
		"type":          "searchset",
		"total":         res.Total,
		"entry":         make([]map[string]any, 0, len(res.Resources)),
	}
	for _, rec := range res.Resources {
		bundle["entry"] = append(bundle["entry"].([]map[string]any), map[string]any{
			"fullUrl":  fmt.Sprintf("/fhir/%s/%s", rec.ResourceType, rec.ID),
			"resource": json.RawMessage(rec.Data),
		})
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(bundle)
}

func (h *FHIRHandler) parseFilters(r *http.Request) storage.SearchFilters {
	f := storage.SearchFilters{
		Count:      h.cfg.DefaultCount,
		Offset:     h.cfg.DefaultOffset,
		MaxCount:   h.cfg.SearchMaxCount,
	}
	for k, v := range r.URL.Query() {
		switch strings.ToLower(k) {
		case "_count":
			if len(v) > 0 {
				if n, err := strconv.Atoi(v[0]); err == nil {
					f.Count = n
				}
			}
		case "_offset":
			if len(v) > 0 {
				if n, err := strconv.Atoi(v[0]); err == nil {
					f.Offset = n
				}
			}
		case "_id":
			if len(v) > 0 {
				f.ID = v[0]
			}
		case "_lastupdated":
			if len(v) > 0 {
				f.LastUpdated = v[0]
			}
		case "name":
			if len(v) > 0 {
				f.Name = v[0]
			}
		case "code":
			if len(v) > 0 {
				f.Code = v[0]
			}
		case "subject":
			if len(v) > 0 {
				f.Subject = v[0]
			}
		}
	}
	return f
}

func (h *FHIRHandler) resolveResourceType(r *http.Request) string {
	if rt := r.Header.Get("X-Resource-Type"); rt != "" {
		return strings.ToLower(rt)
	}
	return ""
}

func (h *FHIRHandler) error(w http.ResponseWriter, r *http.Request, code int, ctx string, err error) {
	// logf disabled
	h.writeOO(w, r, newOO("error", "exception", ctx+": failed"), code)
}

func (h *FHIRHandler) writeOO(w http.ResponseWriter, r *http.Request, oo operationOutcome, code int) {
	w.Header().Set("Content-Type", "application/fhir+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(oo)
}

const FHIR_DATE_FMT = "2006-01-02"

func (h *FHIRHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /fhir/", h.CapabilityStatement)
	mux.HandleFunc("POST /fhir/", h.Create)
	mux.HandleFunc("GET /fhir/_history", h.historyAll)
	mux.HandleFunc("GET /fhir/{type}", h.searchType)
	mux.HandleFunc("GET /fhir/{type}/{id}", h.readLatest)
	mux.HandleFunc("PUT /fhir/{type}/{id}", h.update)
	mux.HandleFunc("DELETE /fhir/{type}/{id}", h.softDelete)
	mux.HandleFunc("GET /fhir/{type}/{id}/_history", h.historyForResource)
	mux.HandleFunc("GET /fhir/{type}/{id}/_history/{version}", h.readVersion)
}

func (h *FHIRHandler) historyAll(w http.ResponseWriter, r *http.Request) {
	res, err := h.store.HistoryAll(r.Context(), storage.SearchFilters{MaxCount: h.cfg.SearchMaxCount, DefaultCount: h.cfg.DefaultCount})
	if err != nil {
		h.writeOO(w, r, newOO("error", "exception", err.Error()), http.StatusInternalServerError)
		return
	}
	bundle := map[string]any{
		"resourceType": "Bundle",
		"type":          "history",
		"total":         res.Total,
		"entry":         make([]map[string]any, 0, len(res.Resources)),
	}
	for _, rec := range res.Resources {
		bundle["entry"] = append(bundle["entry"].([]map[string]any), map[string]any{
			"fullUrl":  fmt.Sprintf("/fhir/%s/%s/_history/%d", rec.ResourceType, rec.ID, rec.Version),
			"request":  map[string]string{"method": "GET", "url": fmt.Sprintf("/fhir/%s/%s/_history/%d", rec.ResourceType, rec.ID, rec.Version)},
			"response": map[string]string{"status": "200 OK"},
			"resource": json.RawMessage(rec.Data),
		})
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(bundle)
}
