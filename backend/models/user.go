package models

import "time"

type User struct {
	ID             string    `firestore:"-" json:"id"`
	Email          string    `firestore:"email" json:"email"`
	FirstName      string    `firestore:"first_name" json:"first_name"`
	LastName       string    `firestore:"last_name" json:"last_name"`
	OrganizationID string    `firestore:"organization_id" json:"organization_id"`
	Role           string    `firestore:"role" json:"role"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
}

type Organization struct {
	ID        string    `firestore:"-" json:"id"`
	Name      string    `firestore:"name" json:"name"`
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
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
