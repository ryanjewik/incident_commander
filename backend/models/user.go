package models

import "time"

type User struct {
	ID                string       `firestore:"-" json:"id"`
	Email             string       `firestore:"email" json:"email"`
	DisplayName       string       `firestore:"display_name" json:"display_name"`
	OrganizationID    string       `firestore:"organization_id" json:"organization_id"`
	Role              string       `firestore:"role" json:"role"`
	CreatedAt         time.Time    `firestore:"created_at" json:"created_at"`
}

type Organization struct {
	ID          string          `firestore:"-" json:"id"`
	Name        string          `firestore:"name" json:"name"`
	CreatedAt   time.Time       `firestore:"created_at" json:"created_at"`
}