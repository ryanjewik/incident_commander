package services

import (
	"context"
	"fmt"
	"log"
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
	log.Printf("[IncidentService] CreateIncident called with orgID=%s, userID=%s", organizationID, userID)

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

	log.Printf("[IncidentService] Creating incident in Firestore: ID=%s, OrgID=%s", incident.ID, incident.OrganizationID)

	_, err := s.firebaseService.Firestore.Collection("incidents").Doc(incident.ID).Set(ctx, incident)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to create incident in Firestore: %v", err)
		return nil, fmt.Errorf("failed to create incident: %w", err)
	}

	log.Printf("[IncidentService] Incident created successfully: ID=%s", incident.ID)
	return incident, nil
}

func (s *IncidentService) GetIncident(ctx context.Context, incidentID, organizationID string) (*models.Incident, error) {
	log.Printf("[IncidentService] GetIncident called with incidentID=%s, orgID=%s", incidentID, organizationID)

	doc, err := s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Get(ctx)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to get incident from Firestore: %v", err)
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
	if v, ok := data["severity_guess"].(string); ok {
		incident.SeverityGuess = v
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
		log.Printf("[IncidentService] ERROR: Access denied - incident orgID=%s does not match requested orgID=%s", incident.OrganizationID, organizationID)
		return nil, fmt.Errorf("access denied")
	}

	// Migrate top-level moderator fields into event object for frontend compatibility
	if incident.Event == nil {
		incident.Event = make(map[string]interface{})
	}
	if moderatorResult, ok := data["moderator_result"].(map[string]interface{}); ok {
		incident.Event["moderator_result"] = moderatorResult
	}
	if moderatorTimestamp, ok := data["moderator_timestamp"].(string); ok {
		incident.Event["moderator_timestamp"] = moderatorTimestamp
	}

	return incident, nil
}

func (s *IncidentService) GetIncidentsByOrganization(ctx context.Context, organizationID string) ([]*models.Incident, error) {
	log.Printf("[IncidentService] ===== GetIncidentsByOrganization START =====")
	log.Printf("[IncidentService] Called with organizationID: '%s' (length: %d)", organizationID, len(organizationID))

	if organizationID == "" {
		log.Printf("[IncidentService] ERROR: organizationID is empty string")
		return nil, fmt.Errorf("organizationID cannot be empty")
	}

	// Check if Firestore client is initialized
	if s.firebaseService == nil {
		log.Printf("[IncidentService] ERROR: firebaseService is nil")
		return nil, fmt.Errorf("firebaseService not initialized")
	}

	if s.firebaseService.Firestore == nil {
		log.Printf("[IncidentService] ERROR: Firestore client is nil")
		return nil, fmt.Errorf("firestore client not initialized")
	}

	log.Printf("[IncidentService] Firestore client verified, creating query...")

	// Query without OrderBy to avoid requiring a composite index
	// We'll sort in memory instead
	query := s.firebaseService.Firestore.Collection("incidents").
		Where("organization_id", "==", organizationID)

	log.Printf("[IncidentService] Query created, executing Documents() call...")

	iter := query.Documents(ctx)
	if iter == nil {
		log.Printf("[IncidentService] ERROR: Documents iterator is nil")
		return nil, fmt.Errorf("failed to create document iterator")
	}

	log.Printf("[IncidentService] Iterator created, starting document iteration...")

	var incidents []*models.Incident
	docCount := 0

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			log.Printf("[IncidentService] Iterator done after %d documents", docCount)
			break
		}
		if err != nil {
			log.Printf("[IncidentService] ERROR: Failed to iterate incidents at doc #%d: %v", docCount+1, err)
			log.Printf("[IncidentService] Error type: %T", err)
			return nil, fmt.Errorf("failed to iterate incidents: %w", err)
		}

		docCount++

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
		if v, ok := data["severity_guess"].(string); ok {
			incident.SeverityGuess = v
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

		// Extract event object if present (contains moderator_result, etc.)
		if eventData, ok := data["event"].(map[string]interface{}); ok {
			incident.Event = eventData
		} else {
			incident.Event = make(map[string]interface{})
		}

		// Migrate top-level moderator fields into event object for frontend compatibility
		if moderatorResult, ok := data["moderator_result"].(map[string]interface{}); ok {
			incident.Event["moderator_result"] = moderatorResult
		}
		if moderatorTimestamp, ok := data["moderator_timestamp"].(string); ok {
			incident.Event["moderator_timestamp"] = moderatorTimestamp
		}

		incidents = append(incidents, incident)
	}

	log.Printf("[IncidentService] Total incidents retrieved: %d", len(incidents))

	// Sort incidents by date in descending order
	sort.Slice(incidents, func(i, j int) bool {
		timeI, errI := time.Parse(time.RFC3339, incidents[i].Date)
		timeJ, errJ := time.Parse(time.RFC3339, incidents[j].Date)

		// If parsing fails, fall back to string comparison
		if errI != nil || errJ != nil {
			return incidents[i].Date > incidents[j].Date
		}

		return timeI.After(timeJ)
	})

	log.Printf("[IncidentService] Incidents sorted successfully")
	log.Printf("[IncidentService] ===== GetIncidentsByOrganization END (returning %d incidents) =====", len(incidents))
	return incidents, nil
}

func (s *IncidentService) UpdateIncident(ctx context.Context, incidentID string, req *models.UpdateIncidentRequest, organizationID string) (*models.Incident, error) {
	log.Printf("[IncidentService] UpdateIncident called with incidentID=%s, orgID=%s", incidentID, organizationID)

	// Verify that the incident exists first and user has access
	incident, err := s.GetIncident(ctx, incidentID, organizationID)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to get incident for update: %v", err)
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

	log.Printf("[IncidentService] Applying %d updates to incident %s", len(updates), incidentID)

	_, err = s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Update(ctx, updates)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to update incident in Firestore: %v", err)
		return nil, fmt.Errorf("failed to update incident: %w", err)
	}

	now := time.Now()
	incident.UpdatedAt = now
	log.Printf("[IncidentService] Incident updated successfully: ID=%s", incidentID)
	return incident, nil
}

func (s *IncidentService) DeleteIncident(ctx context.Context, incidentID, organizationID string) error {
	log.Printf("[IncidentService] DeleteIncident called with incidentID=%s, orgID=%s", incidentID, organizationID)

	// Verify that the incident exists first and user has access
	_, err := s.GetIncident(ctx, incidentID, organizationID)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to get incident for deletion: %v", err)
		return err
	}

	log.Printf("[IncidentService] Deleting incident from Firestore: ID=%s", incidentID)

	_, err = s.firebaseService.Firestore.Collection("incidents").Doc(incidentID).Delete(ctx)
	if err != nil {
		log.Printf("[IncidentService] ERROR: Failed to delete incident from Firestore: %v", err)
		return fmt.Errorf("failed to delete incident: %w", err)
	}

	log.Printf("[IncidentService] Incident deleted successfully: ID=%s", incidentID)
	return nil
}
