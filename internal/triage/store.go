package triage

import (
	"sync"
	"time"
)

type VitalSigns struct {
	SystolicBP   int     `json:"systolic_bp"`
	DiastolicBP  int     `json:"diastolic_bp"`
	HeartRate    int     `json:"heart_rate"`
	RespRate     int     `json:"resp_rate"`
	OxygenSat    int     `json:"oxygen_sat"`
	Temperature  float64 `json:"temperature"`
	RecordedAt   string  `json:"recorded_at"`
}

type Patient struct {
	PatientID      string     `json:"patient_id"`
	PatientName    string     `json:"patient_name"`
	Gender         string     `json:"gender"`
	Age            int        `json:"age"`
	ESI            int        `json:"esi"`
	ChiefComplaint string     `json:"chief_complaint"`
	CheckedInAt    string     `json:"checked_in_at"`
	CheckedOutAt   *string    `json:"checked_out_at,omitempty"`
	Vitals         VitalSigns `json:"vitals,omitempty"`
}

type Store struct {
	mu       sync.RWMutex
	patients map[string]*Patient
}

func NewStore() *Store {
	return &Store{patients: make(map[string]*Patient)}
}

func (s *Store) CheckIn(pid, name, gender string, age int, complaint string) *Patient {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.patients[pid]; ok && existing.CheckedOutAt == nil {
		return existing
	}
	p := &Patient{
		PatientID:      pid,
		PatientName:    name,
		Gender:         gender,
		Age:            age,
		ESI:            3,
		ChiefComplaint: complaint,
		CheckedInAt:    time.Now().UTC().Format(time.RFC3339),
	}
	s.patients[pid] = p
	return p
}

func (s *Store) CheckOut(pid string) *Patient {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.patients[pid]
	if !ok || p.CheckedOutAt != nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p.CheckedOutAt = &now
	return p
}

func (s *Store) SetESI(pid string, esi int) *Patient {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.patients[pid]
	if !ok {
		return nil
	}
	p.ESI = esi
	return p
}

func (s *Store) SetVitals(pid string, v VitalSigns) *Patient {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.patients[pid]
	if !ok {
		return nil
	}
	p.Vitals = v
	return p
}

func (s *Store) Board() []*Patient {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Patient, 0, len(s.patients))
	for _, p := range s.patients {
		result = append(result, p)
	}
	return result
}

func (s *Store) Active() []*Patient {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Patient, 0)
	for _, p := range s.patients {
		if p.CheckedOutAt == nil {
			result = append(result, p)
		}
	}
	return result
}

func (s *Store) Get(pid string) *Patient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.patients[pid]
}