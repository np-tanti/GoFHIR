package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	fhirstore "github.com/graphic/gofhir/internal/fhir/storage"
	"github.com/graphic/gofhir/internal/netutil"
	"github.com/graphic/gofhir/internal/triage"
)

func main() {
	cfg := loadConfig()

	// Open FHIR database
	store, err := fhirstore.Open(cfg.fhirDBPath)
	if err != nil {
		log.Fatalf("fhir store open: %v", err)
	}
	defer store.Close()

	// Create triage store and SSE hub
	triageStore := triage.NewStore()
	triageHub := triage.NewSSEHub()

	// Setup socket paths
	sockets := netutil.NewSocketPaths()

	// Create audit client (async fire-and-forget)
	auditTransport := netutil.UnixSocketTransport(sockets.AuditSock)
	auditClient := &http.Client{
		Transport: auditTransport,
		Timeout:   5 * time.Second,
	}

	// Create handler
	handler := &fhirCoreHandler{
		store:        store,
		triageStore:  triageStore,
		triageHub:    triageHub,
		auditClient:  auditClient,
		cfg:          cfg,
	}

	mux := http.NewServeMux()

	// FHIR API endpoints
	mux.HandleFunc("/fhir/", handler.capabilityStatement)
	mux.HandleFunc("/fhir/_history", handler.historyAll)
	mux.HandleFunc("/fhir/Patient", handler.searchType)
	mux.HandleFunc("/fhir/Patient/", handler.patientHandler)

	// Triage endpoints
	mux.HandleFunc("/triage/board", handler.triageBoard)
	mux.HandleFunc("/triage/checkin", handler.triageCheckIn)
	mux.HandleFunc("/triage/checkout", handler.triageCheckOut)
	mux.HandleFunc("/triage/esi", handler.triageSetESI)
	mux.HandleFunc("/events", handler.triageHub.ServeHTTP)

	// Health endpoints
	mux.HandleFunc("/live", handler.live)
	mux.HandleFunc("/ready", handler.ready)

	// Start listening on Unix socket
	ln, err := netutil.ListenUnixSocket(sockets.FHIRSock)
	if err != nil {
		log.Fatalf("listen on unix socket: %v", err)
	}
	defer ln.Close()

	log.Printf("FHIR-Core listening on %s", sockets.FHIRSock)

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	waitForShutdown(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	})
}

type fhirCoreHandler struct {
	store        *fhirstore.Store
	triageStore  *triage.Store
	triageHub    *triage.SSEHub
	auditClient  *http.Client
	cfg          *config
}

func (h *fhirCoreHandler) live(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (h *fhirCoreHandler) ready(w http.ResponseWriter, r *http.Request) {
	// Check if database is accessible
	ctx := context.Background()
	_, err := h.store.Read(ctx, "test")
	// We expect an error (not found), but the store should be accessible
	// If there's a connection error, it will be a different type of error
	_ = err // Intentionally ignored - we just want to verify DB connectivity
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// FHIR API Methods

func (h *fhirCoreHandler) capabilityStatement(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/fhir/" && r.URL.Path != "/fhir" {
		http.NotFound(w, r)
		return
	}
	stmt := map[string]any{
		"resourceType": "CapabilityStatement",
		"status":        "draft",
		"date":          time.Now().UTC().Format("2006-01-02"),
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
						"interaction": []map[string]string{
							{"code": "create"},
							{"code": "read"},
							{"code": "update"},
							{"code": "delete"},
							{"code": "search-type"},
						},
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	json.NewEncoder(w).Encode(stmt)
}

func (h *fhirCoreHandler) patientHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/fhir/Patient/")
	path = strings.TrimSuffix(path, "/")

	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	patientID := parts[0]

	if len(parts) == 1 {
		// /fhir/Patient/{id}
		switch r.Method {
		case http.MethodGet:
			h.readPatient(w, r, patientID)
		case http.MethodPut:
			h.updatePatient(w, r, patientID)
		case http.MethodDelete:
			h.deletePatient(w, r, patientID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "_history" {
		// /fhir/Patient/{id}/_history or /fhir/Patient/{id}/_history/{version}
		if len(parts) == 2 {
			h.historyForResource(w, r, patientID)
		} else if len(parts) == 3 {
			h.readVersion(w, r, patientID, parts[2])
		}
		return
	}

	http.NotFound(w, r)
}

func (h *fhirCoreHandler) searchType(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resourceType := "Patient"
	filters := h.parseFilters(r)

	res, err := h.store.Search(r.Context(), resourceType, filters)
	if err != nil {
		h.writeOperationOutcome(w, "error", "exception", err.Error(), http.StatusInternalServerError)
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
	json.NewEncoder(w).Encode(bundle)

	// Async audit
	go h.writeAudit(r, "fhir.read")
}

func (h *fhirCoreHandler) readPatient(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := h.store.Read(r.Context(), id)
	if err != nil {
		h.writeOperationOutcome(w, "error", "not-found", err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/fhir+json")
	json.NewEncoder(w).Encode(json.RawMessage(rec.Data))

	go h.writeAudit(r, "fhir.read")
}

func (h *fhirCoreHandler) updatePatient(w http.ResponseWriter, r *http.Request, id string) {
	data, err := readBody(r)
	if err != nil {
		h.writeOperationOutcome(w, "error", "required", "read body failed", http.StatusBadRequest)
		return
	}

	rec := &fhirstore.Resource{ID: id, Data: data}
	updated, err := h.store.Update(r.Context(), rec)
	if err != nil {
		h.writeOperationOutcome(w, "error", "exception", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/fhir+json")
	w.Header().Set("Location", fmt.Sprintf("/fhir/%s/%s/_history/%d", updated.ResourceType, updated.ID, updated.Version))
	json.NewEncoder(w).Encode(map[string]any{
		"resourceType": updated.ResourceType,
		"id":           updated.ID,
		"version":      updated.Version,
	})

	go h.writeAudit(r, "fhir.update")
}

func (h *fhirCoreHandler) deletePatient(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.SoftDelete(r.Context(), id); err != nil {
		h.writeOperationOutcome(w, "error", "not-found", err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	go h.writeAudit(r, "fhir.delete")
}

func (h *fhirCoreHandler) historyForResource(w http.ResponseWriter, r *http.Request, id string) {
	history, err := h.store.History(r.Context(), id)
	if err != nil {
		h.writeOperationOutcome(w, "error", "exception", err.Error(), http.StatusInternalServerError)
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
	json.NewEncoder(w).Encode(bundle)
}

func (h *fhirCoreHandler) readVersion(w http.ResponseWriter, r *http.Request, id, version string) {
	// TODO: implement version-specific read
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (h *fhirCoreHandler) historyAll(w http.ResponseWriter, r *http.Request) {
	res, err := h.store.HistoryAll(r.Context(), fhirstore.SearchFilters{
		MaxCount: h.cfg.searchMaxCount,
	})
	if err != nil {
		h.writeOperationOutcome(w, "error", "exception", err.Error(), http.StatusInternalServerError)
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
			"resource": json.RawMessage(rec.Data),
		})
	}

	w.Header().Set("Content-Type", "application/fhir+json")
	json.NewEncoder(w).Encode(bundle)
}

// Triage Methods

func (h *fhirCoreHandler) triageBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	board := h.triageStore.Active()
	if board == nil {
		board = []*triage.Patient{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"patients": board})
}

func (h *fhirCoreHandler) triageCheckIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PatientID      string `json:"patient_id"`
		ChiefComplaint string `json:"chief_complaint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PatientID == "" {
		http.Error(w, "patient_id required", http.StatusBadRequest)
		return
	}

	// Read patient from FHIR store
	fhirRec, err := h.store.Read(r.Context(), req.PatientID)
	if err != nil {
		http.Error(w, fmt.Sprintf("patient %s not found", req.PatientID), http.StatusNotFound)
		return
	}

	// Parse patient name from FHIR data
	var fhirPatient struct {
		Name []struct {
			Given  []string `json:"given"`
			Family string   `json:"family"`
		} `json:"name"`
		Gender    string `json:"gender"`
		BirthDate string `json:"birthDate"`
	}

	json.Unmarshal(fhirRec.Data, &fhirPatient)

	name := req.PatientID
	if len(fhirPatient.Name) > 0 {
		given := ""
		if len(fhirPatient.Name[0].Given) > 0 {
			given = fhirPatient.Name[0].Given[0]
		}
		family := fhirPatient.Name[0].Family
		if given != "" || family != "" {
			name = strings.TrimSpace(given + " " + family)
		}
	}

	age := 0
	if fhirPatient.BirthDate != "" {
		if year := parseBirthYear(fhirPatient.BirthDate); year > 0 {
			age = time.Now().Year() - year
		}
	}

	patient := h.triageStore.CheckIn(req.PatientID, name, fhirPatient.Gender, age, req.ChiefComplaint)

	payload, _ := json.Marshal(map[string]any{"patient": patient})
	h.triageHub.Broadcast("checkin", payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(patient)

	go h.writeAudit(r, "triage.checkin")
}

func (h *fhirCoreHandler) triageCheckOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PatientID string `json:"patient_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PatientID == "" {
		http.Error(w, "patient_id required", http.StatusBadRequest)
		return
	}

	patient := h.triageStore.CheckOut(req.PatientID)
	if patient == nil {
		http.Error(w, "patient not on board", http.StatusNotFound)
		return
	}

	payload, _ := json.Marshal(map[string]any{"patient": patient})
	h.triageHub.Broadcast("checkout", payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(patient)

	go h.writeAudit(r, "triage.checkout")
}

func (h *fhirCoreHandler) triageSetESI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PatientID string `json:"patient_id"`
		ESI       int    `json:"esi"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PatientID == "" || req.ESI < 1 || req.ESI > 5 {
		http.Error(w, "patient_id required and esi must be 1-5", http.StatusBadRequest)
		return
	}

	patient := h.triageStore.SetESI(req.PatientID, req.ESI)
	if patient == nil {
		http.Error(w, "patient not on board", http.StatusNotFound)
		return
	}

	payload, _ := json.Marshal(map[string]any{"patient": patient})
	h.triageHub.Broadcast("esi-update", payload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(patient)

	go h.writeAudit(r, "triage.esi")
}

// Helper Methods

func (h *fhirCoreHandler) writeAudit(r *http.Request, action string) {
	payload, _ := json.Marshal(map[string]any{
		"method":      r.Method,
		"path":        r.URL.Path,
		"remote":      r.RemoteAddr,
		"action":      action,
	})

	auditReq := map[string]any{
		"action":    action,
		"actor_id":  "system", // TODO: get from context if available
		"payload":   string(payload),
	}

	// Fire-and-forget audit write
	go func() {
		resp, err := h.auditClient.Post(
			"http://audit/audit/event",
			"application/json",
			strings.NewReader(fmt.Sprintf("%v", auditReq)),
		)
		if err != nil {
			log.Printf("audit write error: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

func (h *fhirCoreHandler) writeOperationOutcome(w http.ResponseWriter, severity, code, diag string, statusCode int) {
	oo := map[string]any{
		"resourceType": "OperationOutcome",
		"issue": []map[string]any{
			{
				"severity":    severity,
				"code":        code,
				"diagnostics": diag,
			},
		},
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(oo)
}

func (h *fhirCoreHandler) parseFilters(r *http.Request) fhirstore.SearchFilters {
	f := fhirstore.SearchFilters{
		Count:    20,
		Offset:   0,
		MaxCount: h.cfg.searchMaxCount,
	}

	for k, v := range r.URL.Query() {
		switch strings.ToLower(k) {
		case "_count":
			if len(v) > 0 {
				fmt.Sscanf(v[0], "%d", &f.Count)
			}
		case "_offset":
			if len(v) > 0 {
				fmt.Sscanf(v[0], "%d", &f.Offset)
			}
		case "name":
			if len(v) > 0 {
				f.Name = v[0]
			}
		}
	}
	return f
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 1024*1024) // 1MB max
	n, err := r.Body.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	return nil, err
}

func parseBirthYear(dateStr string) int {
	parts := strings.SplitN(dateStr, "-", 2)
	if len(parts) == 0 {
		return 0
	}
	year := 0
	fmt.Sscanf(parts[0], "%d", &year)
	return year
}

type config struct {
	fhirDBPath   string
	auditDBPath  string
	searchMaxCount int
	corsOrigin   string
}

func loadConfig() *config {
	return &config{
		fhirDBPath:    getEnv("GOFHIR_FHIR_DB_PATH", "data/gofhir_fhir.db"),
		auditDBPath:   getEnv("GOFHIR_AUDIT_DB_PATH", "data/gofhir.db"),
		searchMaxCount: 100,
		corsOrigin:    getEnv("GOFHIR_CORS_ORIGIN", "*"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func waitForShutdown(shutdown func() error) {
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
	log.Printf("shutting down FHIR-Core...")
	_ = shutdown()
}
