package auditor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// FHIR AuditEvent represents a FHIR R4 compliant audit event
// See: https://www.hl7.org/fhir/auditevent.html
type FHIRAuditEvent struct {
	ResourceType string        `json:"resourceType"` // Always "AuditEvent"
	ID           string        `json:"id,omitempty"`
	Type         Coding        `json:"type"` // What type of event
	Subtype      []Coding      `json:"subtype,omitempty"`
	Action       string        `json:"action"` // C/R/U/D/E
	Period       *Period       `json:"period,omitempty"`
	Recorded     string        `json:"recorded"` // Timestamp when recorded (ISO 8601)
	Outcome      string        `json:"outcome"`  // 0=success, 4=minor, 8=serious, 12=major
	OutcomeDesc  string        `json:"outcomeDesc,omitempty"`
	Agent        []AuditAgent  `json:"agent"`  // Who/what performed
	Source       AuditSource   `json:"source"` // Where from
	Entity       []AuditEntity `json:"entity,omitempty"`
}

// Coding represents a FHIR Coding element
type Coding struct {
	System  string `json:"system,omitempty"`
	Code    string `json:"code"`
	Display string `json:"display,omitempty"`
}

// Period represents a FHIR Period
type Period struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// AuditAgent represents who/what performed the event
type AuditAgent struct {
	Type         *CodeableConcept  `json:"type,omitempty"`
	Role         []CodeableConcept `json:"role,omitempty"`
	Reference    *Reference        `json:"reference,omitempty"`
	UserID       *Identifier       `json:"userId,omitempty"`
	AltID        string            `json:"altId,omitempty"`
	Name         string            `json:"name,omitempty"`
	Requestor    bool              `json:"requestor"`
	Location     *Reference        `json:"location,omitempty"`
	Policy       []string          `json:"policy,omitempty"`
	Media        *Coding           `json:"media,omitempty"`
	Network      *AuditNetwork     `json:"network,omitempty"`
	PurposeOfUse []CodeableConcept `json:"purposeOfUse,omitempty"`
}

// CodeableConcept represents a FHIR CodeableConcept
type CodeableConcept struct {
	Coding []Coding `json:"coding,omitempty"`
	Text   string   `json:"text,omitempty"`
}

// Reference represents a FHIR Reference
type Reference struct {
	Reference string `json:"reference,omitempty"`
	Type      string `json:"type,omitempty"`
	Display   string `json:"display,omitempty"`
}

// Identifier represents a FHIR Identifier
type Identifier struct {
	Use    string           `json:"use,omitempty"`
	Type   *CodeableConcept `json:"type,omitempty"`
	System string           `json:"system,omitempty"`
	Value  string           `json:"value,omitempty"`
}

// AuditNetwork represents network information
type AuditNetwork struct {
	Address string `json:"address,omitempty"`
	Type    string `json:"type,omitempty"` // 1=Machine, 2=Application, 3=Process, 4=User
}

// AuditSource represents where the event originated
type AuditSource struct {
	Site   string     `json:"site,omitempty"`
	Type   []Coding   `json:"type,omitempty"`
	Source *Reference `json:"source,omitempty"`
}

// AuditEntity represents what objects were affected
type AuditEntity struct {
	What      *Reference    `json:"what,omitempty"`
	Type      *Coding       `json:"type,omitempty"`
	Role      *Coding       `json:"role,omitempty"`
	Lifecycle *Coding       `json:"lifecycle,omitempty"`
	Security  []Coding      `json:"security,omitempty"`
	Name      string        `json:"name,omitempty"`
	Query     string        `json:"query,omitempty"`
	Detail    []AuditDetail `json:"detail,omitempty"`
}

// AuditDetail represents additional audit details
type AuditDetail struct {
	Type  string      `json:"type"`
	Value interface{} `json:"valueString,omitempty"`
}

// LoginCredentialType represents the type of credential used
type LoginCredentialType string

const (
	CredentialPassword LoginCredentialType = "password"
	CredentialJWT      LoginCredentialType = "jwt"
	CredentialAPIKey   LoginCredentialType = "api-key"
	CredentialMTLS     LoginCredentialType = "mtls"
	CredentialSession  LoginCredentialType = "session"
)

// NewLoginAuditEvent creates a FHIR AuditEvent for login
func NewLoginAuditEvent(userID, username, role, sessionID, remoteAddr, userAgent string, credentialType LoginCredentialType, success bool, failureReason string) FHIRAuditEvent {
	outcome := "0" // success
	outcomeDesc := "Login successful"
	if !success {
		outcome = "8" // serious failure
		outcomeDesc = failureReason
		if outcomeDesc == "" {
			outcomeDesc = "Login failed: invalid credentials"
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	action := "E" // Execute - login is an execution action

	return FHIRAuditEvent{
		ResourceType: "AuditEvent",
		Type: Coding{
			System:  "http://terminology.hl7.org/CodeSystem/audit-event-type",
			Code:    "rest",
			Display: "RESTful Interaction",
		},
		Subtype: []Coding{
			{
				System:  "http://terminology.hl7.org/CodeSystem/audit-event-sub-type",
				Code:    "login",
				Display: "Login",
			},
		},
		Action:      action,
		Recorded:    now,
		Outcome:     outcome,
		OutcomeDesc: outcomeDesc,
		Agent: []AuditAgent{
			{
				Requestor: true,
				UserID: &Identifier{
					System: "urn:ietf:rfc:3986",
					Value:  userID,
				},
				Name: username,
				Role: []CodeableConcept{
					{
						Coding: []Coding{
							{
								System:  "http://terminology.hl7.org/CodeSystem/v3-RoleClass",
								Code:    role,
								Display: role,
							},
						},
					},
				},
				Network: &AuditNetwork{
					Address: remoteAddr,
					Type:    "1", // Machine
				},
				PurposeOfUse: []CodeableConcept{
					{
						Coding: []Coding{
							{
								System:  "http://terminology.hl7.org/CodeSystem/v3-ActReason",
								Code:    "AUTTH",
								Display: "authentication",
							},
						},
					},
				},
			},
		},
		Source: AuditSource{
			Type: []Coding{
				{
					System:  "http://terminology.hl7.org/CodeSystem/security-source-type",
					Code:    "3",
					Display: "Web Server",
				},
			},
		},
		Entity: []AuditEntity{
			{
				Name: "Authentication",
				Type: &Coding{
					System:  "http://terminology.hl7.org/CodeSystem/audit-entity-type",
					Code:    "2",
					Display: "System Object",
				},
				Detail: []AuditDetail{
					{
						Type:  "credentialType",
						Value: string(credentialType),
					},
					{
						Type:  "sessionId",
						Value: sessionID,
					},
					{
						Type:  "userAgent",
						Value: userAgent,
					},
				},
			},
		},
	}
}

// NewLogoutAuditEvent creates a FHIR AuditEvent for logout
func NewLogoutAuditEvent(userID, username, role, sessionID, remoteAddr string) FHIRAuditEvent {
	now := time.Now().UTC().Format(time.RFC3339)

	return FHIRAuditEvent{
		ResourceType: "AuditEvent",
		Type: Coding{
			System:  "http://terminology.hl7.org/CodeSystem/audit-event-type",
			Code:    "rest",
			Display: "RESTful Interaction",
		},
		Subtype: []Coding{
			{
				System:  "http://terminology.hl7.org/CodeSystem/audit-event-sub-type",
				Code:    "logout",
				Display: "Logout",
			},
		},
		Action:      "E",
		Recorded:    now,
		Outcome:     "0",
		OutcomeDesc: "Logout successful",
		Agent: []AuditAgent{
			{
				Requestor: true,
				UserID: &Identifier{
					System: "urn:ietf:rfc:3986",
					Value:  userID,
				},
				Name: username,
				Role: []CodeableConcept{
					{
						Coding: []Coding{
							{
								System:  "http://terminology.hl7.org/CodeSystem/v3-RoleClass",
								Code:    role,
								Display: role,
							},
						},
					},
				},
				Network: &AuditNetwork{
					Address: remoteAddr,
					Type:    "1",
				},
				PurposeOfUse: []CodeableConcept{
					{
						Coding: []Coding{
							{
								System:  "http://terminology.hl7.org/CodeSystem/v3-ActReason",
								Code:    "AUTTH",
								Display: "authentication",
							},
						},
					},
				},
			},
		},
		Source: AuditSource{
			Type: []Coding{
				{
					System:  "http://terminology.hl7.org/CodeSystem/security-source-type",
					Code:    "3",
					Display: "Web Server",
				},
			},
		},
		Entity: []AuditEntity{
			{
				Name: "Session",
				Type: &Coding{
					System:  "http://terminology.hl7.org/CodeSystem/audit-entity-type",
					Code:    "2",
					Display: "System Object",
				},
				Detail: []AuditDetail{
					{
						Type:  "sessionId",
						Value: sessionID,
					},
				},
			},
		},
	}
}

// ToJSON converts the AuditEvent to JSON
func (e *FHIRAuditEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ComputeHash computes a SHA-256 hash of the audit event for integrity
func (e *FHIRAuditEvent) ComputeHash() ([32]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return [32]byte{}, fmt.Errorf("marshal audit event: %w", err)
	}
	return sha256.Sum256(data), nil
}
