package models

import "time"

// Incident represents an incident in the system
type Incident struct {
	ID             string                 `firestore:"id" json:"id"`
	OrganizationID string                 `firestore:"organization_id" json:"organization_id"`
	Title          string                 `firestore:"title" json:"title"`
	Status         string                 `firestore:"status" json:"status"` // Active, Resolved, Ignored, New
	Date           string                 `firestore:"date" json:"date"`
	Type           string                 `firestore:"type" json:"type"` // Incident Report, NL Query
	Description    string                 `firestore:"description" json:"description"`
	CreatedBy      string                 `firestore:"created_by" json:"created_by"` // User ID
	CreatedAt      time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt      time.Time              `firestore:"updated_at" json:"updated_at"`
	Metadata       map[string]interface{} `firestore:"metadata,omitempty" json:"metadata,omitempty"`
}

// CreateIncidentRequest is the request body for creating an incident
type CreateIncidentRequest struct {
	Title       string                 `json:"title" binding:"required"`
	Status      string                 `json:"status"`
	Type        string                 `json:"type" binding:"required"`
	Description string                 `json:"description" binding:"required"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateIncidentRequest is the request body for updating an incident
type UpdateIncidentRequest struct {
	Title       string                 `json:"title,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}
