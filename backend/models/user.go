package models

import "time"

type User struct {
	ID             string    `firestore:"id" json:"id"`
	FirebaseUID    string    `firestore:"firebase_uid" json:"firebase_uid"`
	Email          string    `firestore:"email" json:"email"`
	FirstName      string    `firestore:"first_name" json:"first_name"`
	LastName       string    `firestore:"last_name" json:"last_name"`
	OrganizationID string    `firestore:"organization_id" json:"organization_id"`
	Role           string    `firestore:"role" json:"role"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt      time.Time `firestore:"updated_at" json:"updated_at"`
}

type Organization struct {
	ID              string                 `firestore:"-" json:"id"`
	Name            string                 `firestore:"name" json:"name"`
	CreatedAt       time.Time              `firestore:"created_at" json:"created_at"`
	DatadogSettings map[string]interface{} `firestore:"datadog_settings" json:"datadog_settings,omitempty"`
	DatadogSecrets  map[string]interface{} `firestore:"datadog_secrets" json:"datadog_secrets,omitempty"`
}

type JoinRequest struct {
	ID             string    `firestore:"-" json:"id"`
	UserID         string    `firestore:"user_id" json:"user_id"`
	UserEmail      string    `firestore:"user_email" json:"user_email"`
	UserFirstName  string    `firestore:"user_first_name" json:"user_first_name"`
	UserLastName   string    `firestore:"user_last_name" json:"user_last_name"`
	OrganizationID string    `firestore:"organization_id" json:"organization_id"`
	Status         string    `firestore:"status" json:"status"` // pending, approved, rejected
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt      time.Time `firestore:"updated_at" json:"updated_at"`
}

// Membership represents a user's membership in an organization.
// Users may have multiple memberships; `User.OrganizationID` remains the active organization.
type Membership struct {
	ID             string    `firestore:"-" json:"id"`
	UserID         string    `firestore:"user_id" json:"user_id"`
	OrganizationID string    `firestore:"organization_id" json:"organization_id"`
	Role           string    `firestore:"role" json:"role"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
}

type Message struct {
	ID             string    `firestore:"-" json:"id"`
	OrganizationID string    `firestore:"organization_id" json:"organization_id"`
	IncidentID     string    `firestore:"incident_id" json:"incident_id"`
	UserID         string    `firestore:"user_id" json:"user_id"`
	UserName       string    `firestore:"user_name" json:"user_name"`
	Text           string    `firestore:"text" json:"text"`
	MentionsBot    bool      `firestore:"mentions_bot" json:"mentions_bot"`
	BotResponse    string    `firestore:"bot_response,omitempty" json:"bot_response,omitempty"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
}
