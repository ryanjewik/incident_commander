package models

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// ModeratorResult represents the AI moderator's analysis of an incident
type ModeratorResult struct {
	Confidence          float64 `firestore:"confidence" json:"confidence"`
	IncidentSummary     string  `firestore:"incident_summary" json:"incident_summary"`
	MostLikelyRootCause string  `firestore:"most_likely_root_cause" json:"most_likely_root_cause"`
	Reasoning           string  `firestore:"reasoning" json:"reasoning"`
	SeverityGuess       string  `firestore:"severity_guess" json:"severity_guess"`
}

// FlexibleTime handles both string and time.Time from Firestore
type FlexibleTime struct {
	time.Time
}

// UnmarshalJSON handles both string and time formats
func (ft *FlexibleTime) UnmarshalJSON(data []byte) error {
	// Try parsing as time.Time first
	var t time.Time
	if err := json.Unmarshal(data, &t); err == nil {
		ft.Time = t
		return nil
	}

	// Try parsing as string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			ft.Time = time.Time{}
			return nil
		}
		// Try RFC3339 format
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			ft.Time = t
			return nil
		}
		// Try other common formats if needed
		t, err = time.Parse(time.RFC3339Nano, s)
		if err == nil {
			ft.Time = t
			return nil
		}
		return err
	}

	return nil
}

// MarshalJSON outputs as time.Time
func (ft FlexibleTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ft.Time)
}

// UnmarshalFirestoreValue implements firestore deserialization
func (ft *FlexibleTime) UnmarshalFirestoreValue(val interface{}) error {
	switch v := val.(type) {
	case *timestamppb.Timestamp:
		if v != nil {
			ft.Time = v.AsTime()
		} else {
			ft.Time = time.Time{}
		}
		return nil
	case time.Time:
		ft.Time = v
		return nil
	case string:
		if v == "" {
			ft.Time = time.Time{}
			return nil
		}
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			ft.Time = t
			return nil
		}
		t, err = time.Parse(time.RFC3339Nano, v)
		if err == nil {
			ft.Time = t
			return nil
		}
		return err
	case nil:
		ft.Time = time.Time{}
		return nil
	default:
		return fmt.Errorf("unexpected type for FlexibleTime: %T", val)
	}
}

// MarshalFirestoreValue implements firestore serialization
func (ft FlexibleTime) MarshalFirestoreValue() (interface{}, error) {
	return ft.Time, nil
}

// Incident represents an incident in the system
type Incident struct {
	ID             string                 `firestore:"id" json:"id"`
	OrganizationID string                 `firestore:"organization_id" json:"organization_id"`
	AlertID        string                 `firestore:"alert_id,omitempty" json:"alert_id,omitempty"`
	Title          string                 `firestore:"title" json:"title"`
	Status         string                 `firestore:"status" json:"status"` // Active, Resolved, Ignored, New
	Date           string                 `firestore:"date" json:"date"`
	Type           string                 `firestore:"type" json:"type"` // Incident Report, NL Query
	Description    string                 `firestore:"description" json:"description"`
	CreatedBy      string                 `firestore:"created_by" json:"created_by"` // User ID
	CreatedAt      time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt      time.Time              `firestore:"updated_at" json:"updated_at"`
	SeverityGuess  string                 `firestore:"severity_guess,omitempty" json:"severity_guess,omitempty"`
	Metadata       map[string]interface{} `firestore:"metadata,omitempty" json:"metadata,omitempty"`
	Event          map[string]interface{} `firestore:"event,omitempty" json:"event"`
	// Historical moderator analyses appended by backend
	ModeratorHistory []map[string]interface{} `firestore:"moderator_history,omitempty" json:"moderator_history,omitempty"`
}

// CreateIncidentRequest is the request body for creating an incident
type CreateIncidentRequest struct {
	Title       string                 `json:"title" binding:"required"`
	Status      string                 `json:"status"`
	AlertID     string                 `json:"alert_id,omitempty"`
	Type        string                 `json:"type" binding:"required"`
	Description string                 `json:"description" binding:"required"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateIncidentRequest is the request body for updating an incident
type UpdateIncidentRequest struct {
	Title         string                 `json:"title,omitempty"`
	Status        string                 `json:"status,omitempty"`
	SeverityGuess string                 `json:"severity_guess,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}
