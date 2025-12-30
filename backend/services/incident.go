package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/ryanjewik/incident_commander/backend/models"
	"google.golang.org/api/iterator"
)

type IncidentService struct {
	firebaseService *FirebaseService
}

func NewIncidentService(firebaseService *FirebaseService) *IncidentService {
	return &IncidentService{
		firebaseService: firebaseService,
	}
}

func (s *IncidentService) CreateIncident(ctx context.Context, req *models.CreateIncidentRequest, organizationID, userID string) (*models.Incident, error) {
	now := time.Now()
	incident := &models.Incident{
		ID:             uuid.New().String(),
		OrganizationID: organizationID,
		Title:          req.Title,
		AlertID:        req.AlertID,
		Status:         req.Status,
		Date:           now.Format(time.RFC3339),
		Type:           req.Type,
		Description:    req.Description,
		CreatedBy:      userID,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       req.Metadata,
	}

	if incident.Status == "" {
		incident.Status = "New"
	}

	_, err := s.firebaseService.Firestore.Collection("incidents").Doc(incident.ID).Set(ctx, incident)
	if err != nil {
		return nil, fmt.Errorf("failed to create incident: %w", err)
	}

	return incident, nil
}

func (s *IncidentService) GetIncident(ctx context.Context, incidentID, organizationID string) (*models.Incident, error) {
	doc, err := s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get incident: %w", err)
	}

	data := doc.Data()
	incident := &models.Incident{}
	if v, ok := data["id"].(string); ok {
		incident.ID = v
	} else {
		incident.ID = doc.Ref.ID
	}
	if v, ok := data["organization_id"].(string); ok {
		incident.OrganizationID = v
	}
	if v, ok := data["alert_id"].(string); ok {
		incident.AlertID = v
	}
	if v, ok := data["title"].(string); ok {
		incident.Title = v
	}
	if v, ok := data["status"].(string); ok {
		incident.Status = v
	}
	if v, ok := data["date"].(string); ok {
		incident.Date = v
	}
	if v, ok := data["type"].(string); ok {
		incident.Type = v
	}
	if v, ok := data["description"].(string); ok {
		incident.Description = v
	}
	if v, ok := data["created_by"].(string); ok {
		incident.CreatedBy = v
	}

	if v, ok := data["created_at"].(time.Time); ok {
		incident.CreatedAt = v
	} else if v, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			incident.CreatedAt = t
		}
	}

	if v, ok := data["updated_at"].(time.Time); ok {
		incident.UpdatedAt = v
	} else if v, ok := data["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			incident.UpdatedAt = t
		}
	}

	if v, ok := data["metadata"].(map[string]interface{}); ok {
		incident.Metadata = v
	}

	if incident.OrganizationID != organizationID {
		return nil, fmt.Errorf("access denied")
	}

	return incident, nil
}

func (s *IncidentService) GetIncidentsByOrganization(ctx context.Context, organizationID string) ([]*models.Incident, error) {
	// Query without OrderBy to avoid requiring a composite index
	// We'll sort in memory instead
	iter := s.firebaseService.Firestore.Collection("incidents").
		Where("organization_id", "==", organizationID).
		Documents(ctx)

	var incidents []*models.Incident
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate incidents: %w", err)
		}

		// Defensive parsing: Firestore documents may have slightly different
		// types for timestamp fields (string vs time.Time). Read the raw
		// data map and convert fields explicitly to avoid DataTo failures
		// that cause a 500 response.
		data := doc.Data()

		incident := &models.Incident{}
		if v, ok := data["id"].(string); ok {
			incident.ID = v
		} else {
			incident.ID = doc.Ref.ID
		}
		if v, ok := data["organization_id"].(string); ok {
			incident.OrganizationID = v
		}
		if v, ok := data["alert_id"].(string); ok {
			incident.AlertID = v
		}
		if v, ok := data["title"].(string); ok {
			incident.Title = v
		}
		if v, ok := data["status"].(string); ok {
			incident.Status = v
		}
		if v, ok := data["date"].(string); ok {
			incident.Date = v
		}
		if v, ok := data["type"].(string); ok {
			incident.Type = v
		}
		if v, ok := data["description"].(string); ok {
			incident.Description = v
		}
		if v, ok := data["created_by"].(string); ok {
			incident.CreatedBy = v
		}

		// created_at / updated_at may come as time.Time or as a string
		if v, ok := data["created_at"].(time.Time); ok {
			incident.CreatedAt = v
		} else if v, ok := data["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				incident.CreatedAt = t
			}
		}

		if v, ok := data["updated_at"].(time.Time); ok {
			incident.UpdatedAt = v
		} else if v, ok := data["updated_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				incident.UpdatedAt = t
			}
		}

		if v, ok := data["metadata"].(map[string]interface{}); ok {
			incident.Metadata = v
		}

		incidents = append(incidents, incident)
	}

	// Sort in memory by date descending
	sort.Slice(incidents, func(i, j int) bool {
		timeI, errI := time.Parse(time.RFC3339, incidents[i].Date)
		timeJ, errJ := time.Parse(time.RFC3339, incidents[j].Date)

		// If parsing fails, fall back to string comparison
		if errI != nil || errJ != nil {
			return incidents[i].Date > incidents[j].Date
		}

		return timeI.After(timeJ)
	})

	return incidents, nil
}

func (s *IncidentService) UpdateIncident(ctx context.Context, incidentID string, req *models.UpdateIncidentRequest, organizationID string) (*models.Incident, error) {
	// Verify that the incident exists first and user has access
	incident, err := s.GetIncident(ctx, incidentID, organizationID)
	if err != nil {
		return nil, err
	}
	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now()},
	}

	if req.Title != "" {
		updates = append(updates, firestore.Update{Path: "title", Value: req.Title})
		incident.Title = req.Title
	}
	if req.Status != "" {
		updates = append(updates, firestore.Update{Path: "status", Value: req.Status})
		incident.Status = req.Status
	}
	if req.Description != "" {
		updates = append(updates, firestore.Update{Path: "description", Value: req.Description})
		incident.Description = req.Description
	}

	if req.SeverityGuess != "" {
		updates = append(updates, firestore.Update{Path: "severity_guess", Value: req.SeverityGuess})
		incident.SeverityGuess = req.SeverityGuess
	}
	if req.Metadata != nil {
		updates = append(updates, firestore.Update{Path: "metadata", Value: req.Metadata})
		incident.Metadata = req.Metadata
	}

	_, err = s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Update(ctx, updates)
	if err != nil {
		return nil, fmt.Errorf("failed to update incident: %w", err)
	}

	incident.UpdatedAt = time.Now()
	return incident, nil
}

func (s *IncidentService) DeleteIncident(ctx context.Context, incidentID, organizationID string) error {
	// Verify that the incident exists first and user has access
	_, err := s.GetIncident(ctx, incidentID, organizationID)
	if err != nil {
		return err
	}
	_, err = s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete incident: %w", err)
	}
	return nil
}
