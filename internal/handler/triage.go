package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	fhirstore "github.com/graphic/gofhir/internal/fhir/storage"
	"github.com/graphic/gofhir/internal/triage"
)

type TriageHandler struct {
	store *triage.Store
	hub   *triage.SSEHub
	fhir  *fhirstore.Store
}

func NewTriageHandler(ts *triage.Store, hub *triage.SSEHub, fhir *fhirstore.Store) *TriageHandler {
	return &TriageHandler{store: ts, hub: hub, fhir: fhir}
}

func (h *TriageHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /triage/board", h.Board)
	mux.HandleFunc("POST /triage/checkin", h.CheckIn)
	mux.HandleFunc("POST /triage/checkout", h.CheckOut)
	mux.HandleFunc("POST /triage/esi", h.SetESI)
	mux.HandleFunc("GET /events", h.hub.ServeHTTP)
}

func triageWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func triageWriteError(w http.ResponseWriter, status int, msg string) {
	triageWriteJSON(w, status, map[string]string{"error": msg})
}

func (h *TriageHandler) Board(w http.ResponseWriter, r *http.Request) {
	board := h.store.Active()
	if board == nil {
		board = []*triage.Patient{}
	}
	triageWriteJSON(w, http.StatusOK, map[string]any{"patients": board})
}

func (h *TriageHandler) CheckIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PatientID      string `json:"patient_id"`
		ChiefComplaint string `json:"chief_complaint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		triageWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.PatientID == "" {
		triageWriteError(w, http.StatusBadRequest, "patient_id required")
		return
	}

	fhirRec, err := h.fhir.Read(r.Context(), req.PatientID)
	if err != nil {
		triageWriteError(w, http.StatusNotFound, fmt.Sprintf("patient %s not found in FHIR store", req.PatientID))
		return
	}

	var fhirPatient struct {
		Name []struct {
			Given  []string `json:"given"`
			Family string   `json:"family"`
		} `json:"name"`
		Gender    string `json:"gender"`
		BirthDate string `json:"birthDate"`
	}
	_ = json.Unmarshal(fhirRec.Data, &fhirPatient)

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
			age = 2026 - year
		}
	}

	patient := h.store.CheckIn(req.PatientID, name, fhirPatient.Gender, age, req.ChiefComplaint)

	payload, _ := json.Marshal(map[string]any{
		"patient": patient,
	})
	h.hub.Broadcast("checkin", payload)

	triageWriteJSON(w, http.StatusOK, patient)
}

func (h *TriageHandler) CheckOut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PatientID string `json:"patient_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		triageWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.PatientID == "" {
		triageWriteError(w, http.StatusBadRequest, "patient_id required")
		return
	}

	patient := h.store.CheckOut(req.PatientID)
	if patient == nil {
		triageWriteError(w, http.StatusNotFound, "patient not on board or already checked out")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"patient": patient,
	})
	h.hub.Broadcast("checkout", payload)

	triageWriteJSON(w, http.StatusOK, patient)
}

func (h *TriageHandler) SetESI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PatientID string `json:"patient_id"`
		ESI       int    `json:"esi"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		triageWriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.PatientID == "" || req.ESI < 1 || req.ESI > 5 {
		triageWriteError(w, http.StatusBadRequest, "patient_id required and esi must be 1-5")
		return
	}

	patient := h.store.SetESI(req.PatientID, req.ESI)
	if patient == nil {
		triageWriteError(w, http.StatusNotFound, "patient not on board")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"patient": patient,
	})
	h.hub.Broadcast("esi-update", payload)

	triageWriteJSON(w, http.StatusOK, patient)
}

func parseBirthYear(dateStr string) int {
	parts := strings.SplitN(dateStr, "-", 2)
	if len(parts) == 0 {
		return 0
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return year
}