package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"firebase.google.com/go/v4/auth"
	"github.com/ryanjewik/incident_commander/backend/models"
)

type UserService struct {
	fs *FirebaseService
}

func NewUserService(fs *FirebaseService) *UserService {
	return &UserService{fs: fs}
}

func (us *UserService) CreateUser(ctx context.Context, email, password, firstName, lastName string) (*models.User, error) {
	params := (&auth.UserToCreate{}).
		Email(email).
		Password(password).
		DisplayName(firstName + " " + lastName)

	authUser, err := us.fs.Auth.CreateUser(ctx, params)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		ID:             authUser.UID,
		Email:          email,
		FirstName:      firstName,
		LastName:       lastName,
		OrganizationID: "",
		Role:           "",
		CreatedAt:      time.Now(),
	}

	_, err = us.fs.Firestore.Collection("users").Doc(authUser.UID).Set(ctx, user)
	if err != nil {
		us.fs.Auth.DeleteUser(ctx, authUser.UID)
		return nil, err
	}

	return user, nil
}

func (us *UserService) CreateUserRecord(ctx context.Context, userID, email, displayName string) (*models.User, error) {
	// Create Firestore record for existing Firebase Auth user
	// Parse displayName to get firstName and lastName
	firstName := displayName
	lastName := ""
	if displayName != "" {
		// Simple split - take first word as firstName, rest as lastName
		parts := []rune(displayName)
		for i, r := range parts {
			if r == ' ' && i > 0 {
				firstName = string(parts[:i])
				if i+1 < len(parts) {
					lastName = string(parts[i+1:])
				}
				break
			}
		}
	}

	user := &models.User{
		ID:             userID,
		Email:          email,
		FirstName:      firstName,
		LastName:       lastName,
		OrganizationID: "",
		Role:           "",
		CreatedAt:      time.Now(),
	}

	_, err := us.fs.Firestore.Collection("users").Doc(userID).Set(ctx, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (us *UserService) GetUser(ctx context.Context, userID string) (*models.User, error) {
	doc, err := us.fs.Firestore.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		return nil, err
	}

	var user models.User
	if err := doc.DataTo(&user); err != nil {
		return nil, err
	}
	user.ID = doc.Ref.ID

	return &user, nil
}

func (us *UserService) GetUsersByOrg(ctx context.Context, orgID string) ([]*models.User, error) {
	// Query memberships for this organization, then fetch user documents
	iter := us.fs.Firestore.Collection("memberships").
		Where("organization_id", "==", orgID).
		Documents(ctx)

	var users []*models.User
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}

		var membership models.Membership
		if err := doc.DataTo(&membership); err != nil {
			continue
		}

		userDoc, err := us.fs.Firestore.Collection("users").Doc(membership.UserID).Get(ctx)
		if err != nil {
			continue
		}
		var user models.User
		if err := userDoc.DataTo(&user); err != nil {
			continue
		}
		user.ID = userDoc.Ref.ID
		// Always use role from membership since roles are per-organization
		user.Role = membership.Role
		users = append(users, &user)
	}

	return users, nil
}

func (us *UserService) VerifyToken(ctx context.Context, idToken string) (*auth.Token, error) {
	return us.fs.Auth.VerifyIDToken(ctx, idToken)
}

func (us *UserService) GetAuthUser(ctx context.Context, userID string) (*auth.UserRecord, error) {
	return us.fs.Auth.GetUser(ctx, userID)
}

func (us *UserService) GetAuthUserByEmail(ctx context.Context, email string) (*auth.UserRecord, error) {
	return us.fs.Auth.GetUserByEmail(ctx, email)
}

func (us *UserService) CreateOrganization(ctx context.Context, name, creatorID string) (*models.Organization, error) {
	// Check if organization name already exists
	iter := us.fs.Firestore.Collection("organizations").Where("name", "==", name).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == nil && doc != nil {
		return nil, errors.New("organization with this name already exists")
	}

	org := &models.Organization{
		Name:      name,
		CreatedAt: time.Now(),
	}

	ref, _, err := us.fs.Firestore.Collection("organizations").Add(ctx, org)
	if err != nil {
		return nil, err
	}

	org.ID = ref.ID

	// Add creator as admin member in memberships collection
	if err := us.AddUserToOrg(ctx, creatorID, org.ID, "admin"); err != nil {
		return nil, err
	}

	// Always set the newly created org as active and update role to admin
	_, err = us.fs.Firestore.Collection("users").Doc(creatorID).Update(ctx, []firestore.Update{
		{Path: "organization_id", Value: org.ID},
		{Path: "role", Value: "admin"},
	})
	if err != nil {
		return nil, err
	}

	return org, nil
}

func (us *UserService) GetOrganization(ctx context.Context, orgID string) (*models.Organization, error) {
	doc, err := us.fs.Firestore.Collection("organizations").Doc(orgID).Get(ctx)
	if err != nil {
		return nil, err
	}

	var org models.Organization
	if err := doc.DataTo(&org); err != nil {
		return nil, err
	}
	org.ID = doc.Ref.ID

	return &org, nil
}

func (us *UserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	authUser, err := us.fs.Auth.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	return us.GetUser(ctx, authUser.UID)
}

func (us *UserService) AddUserToOrg(ctx context.Context, userID, orgID, role string) error {
	// Upsert membership: update role if membership already exists, otherwise create
	docs, err := us.fs.Firestore.Collection("memberships").
		Where("user_id", "==", userID).
		Where("organization_id", "==", orgID).
		Documents(ctx).GetAll()
	if err != nil {
		return err
	}

	if len(docs) > 0 {
		_, err = docs[0].Ref.Update(ctx, []firestore.Update{{Path: "role", Value: role}})
		if err != nil {
			return err
		}
	} else {
		_, _, err = us.fs.Firestore.Collection("memberships").Add(ctx, &models.Membership{
			UserID:         userID,
			OrganizationID: orgID,
			Role:           role,
			CreatedAt:      time.Now(),
		})
		if err != nil {
			return err
		}
	}
	// If user has no active organization, set this as active
	user, err := us.GetUser(ctx, userID)
	if err == nil && (user.OrganizationID == "" || user.OrganizationID == "default") {
		_, _ = us.fs.Firestore.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
			{Path: "organization_id", Value: orgID},
			{Path: "role", Value: role},
		})
	}
	return nil
}

func (us *UserService) RemoveUserFromOrg(ctx context.Context, userID, orgID string) error {
	// Delete membership for this user/org pair
	iter := us.fs.Firestore.Collection("memberships").
		Where("user_id", "==", userID).
		Where("organization_id", "==", orgID).
		Documents(ctx)
	defer iter.Stop()
	for {
		d, err := iter.Next()
		if err != nil {
			break
		}
		_, _ = d.Ref.Delete(ctx)
	}
	// If active org equals removed org, clear active org
	user, err := us.GetUser(ctx, userID)
	if err == nil && user.OrganizationID == orgID {
		_, _ = us.fs.Firestore.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
			{Path: "organization_id", Value: ""},
			{Path: "role", Value: ""},
		})
	}
	return nil
}

// GetMembership returns the membership for a user in a specific organization, or nil if none
func (us *UserService) GetMembership(ctx context.Context, userID, orgID string) (*models.Membership, error) {
	docs, err := us.fs.Firestore.Collection("memberships").
		Where("user_id", "==", userID).
		Where("organization_id", "==", orgID).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	var membership models.Membership
	if err := docs[0].DataTo(&membership); err != nil {
		return nil, err
	}
	membership.ID = docs[0].Ref.ID
	return &membership, nil
}

// GetUserOrganizations returns organizations the user is a member of
func (us *UserService) GetUserOrganizations(ctx context.Context, userID string) ([]*models.Organization, error) {
	iter := us.fs.Firestore.Collection("memberships").
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	var orgs []*models.Organization
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var m models.Membership
		if err := doc.DataTo(&m); err != nil {
			continue
		}
		orgDoc, err := us.fs.Firestore.Collection("organizations").Doc(m.OrganizationID).Get(ctx)
		if err != nil {
			continue
		}
		var org models.Organization
		if err := orgDoc.DataTo(&org); err != nil {
			continue
		}
		org.ID = orgDoc.Ref.ID
		orgs = append(orgs, &org)
	}
	return orgs, nil
}

// SetActiveOrganization sets user's active organization if they have membership
func (us *UserService) SetActiveOrganization(ctx context.Context, userID, orgID string) error {
	// Verify membership exists
	iter := us.fs.Firestore.Collection("memberships").
		Where("user_id", "==", userID).
		Where("organization_id", "==", orgID).
		Documents(ctx)
	defer iter.Stop()
	_, err := iter.Next()
	if err != nil {
		return errors.New("membership not found for selected organization")
	}
	_, err = us.fs.Firestore.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "organization_id", Value: orgID},
	})
	return err
}

func (us *UserService) SearchOrganizations(ctx context.Context, query string) ([]*models.Organization, error) {
	iter := us.fs.Firestore.Collection("organizations").Documents(ctx)
	defer iter.Stop()

	var organizations []*models.Organization
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}

		var org models.Organization
		if err := doc.DataTo(&org); err != nil {
			continue
		}
		org.ID = doc.Ref.ID

		// Filter by query if provided (case-insensitive substring match)
		if query != "" {
			lowerQuery := strings.ToLower(query)
			lowerName := strings.ToLower(org.Name)
			if !strings.Contains(lowerName, lowerQuery) {
				continue
			}
		}

		organizations = append(organizations, &org)
	}

	return organizations, nil
}

// UpdateOrganizationDatadog updates the organization document with datadog secrets (masked) and settings
func (us *UserService) UpdateOrganizationDatadog(ctx context.Context, orgID string, secrets map[string]interface{}, settings map[string]interface{}) error {
	// Build per-field updates so we don't accidentally wipe existing values when keys are omitted
	updates := make([]firestore.Update, 0)

	if secrets != nil {
		// Handle webhookSecret: hash and store the hash only when provided and non-empty
		if wsRaw, ok := secrets["webhookSecret"]; ok {
			if ws, ok2 := wsRaw.(string); ok2 && strings.TrimSpace(ws) != "" {
				hashed, err := HashWebhookSecret(ws)
				if err != nil {
					log.Printf("HashWebhookSecret error: %v", err)
					return err
				}
				updates = append(updates, firestore.Update{Path: "datadog_secrets.webhookSecret", Value: hashed})
			}
		}

		// Handle clientId for OAuth token issuance (use webhookSecret as the client secret)
		if cidRaw, ok := secrets["client_id"]; ok {
			if cid, ok2 := cidRaw.(string); ok2 && strings.TrimSpace(cid) != "" {
				updates = append(updates, firestore.Update{Path: "datadog_secrets.client_id", Value: cid})
			}
		}

		// If both apiKey and appKey are provided and non-empty, encrypt them before saving
		apiRaw, apiOk := secrets["apiKey"]
		appRaw, appOk := secrets["appKey"]
		if apiOk && appOk {
			if apiKey, ok1 := apiRaw.(string); ok1 {
				if appKey, ok2 := appRaw.(string); ok2 {
					if strings.TrimSpace(apiKey) != "" && strings.TrimSpace(appKey) != "" {
						encryptedApiKey, encryptedAppKey, encryptedDek, err := EncryptData(apiKey, appKey)
						if err != nil {
							log.Printf("EncryptData error: %v", err)
							return err
						}
						// store encrypted blobs and wrapped DEK as nested fields
						updates = append(updates,
							firestore.Update{Path: "datadog_secrets.encrypted_apiKey", Value: encryptedApiKey},
							firestore.Update{Path: "datadog_secrets.encrypted_appKey", Value: encryptedAppKey},
							firestore.Update{Path: "datadog_secrets.encrypted_apiKeyDek", Value: encryptedDek},
						)
					}
				}
			}
		}
	}

	// Apply settings per-field to avoid replacing the map unintentionally
	if settings != nil {
		// Note: `securitySignals` toggle removed — treat missing toggles as false by default
		toggleKeys := []string{"systemMetrics", "alertStatus", "activityFeed", "liveLogs"}
		for _, k := range toggleKeys {
			if v, ok := settings[k]; ok {
				// coerce to bool when possible
				b := false
				switch t := v.(type) {
				case bool:
					b = t
				case *bool:
					if t != nil {
						b = *t
					}
				case string:
					b = strings.ToLower(t) == "true"
				case float64:
					b = t != 0
				default:
					// keep b false
				}
				updates = append(updates, firestore.Update{Path: fmt.Sprintf("datadog_settings.%s", k), Value: b})
			}
		}
	}

	if len(updates) == 0 {
		// nothing to update
		return nil
	}

	_, err := us.fs.Firestore.Collection("organizations").Doc(orgID).Update(ctx, updates)
	return err
}

func (us *UserService) CreateJoinRequest(ctx context.Context, userID, organizationID string) (*models.JoinRequest, error) {
	// Fetch user for metadata
	user, err := us.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Allow creating requests regardless of active org; prevent duplicates only for same org pending
	// Check if there's already a pending request
	existingRequests, err := us.fs.Firestore.Collection("join_requests").
		Where("user_id", "==", userID).
		Where("organization_id", "==", organizationID).
		Where("status", "==", "pending").
		Documents(ctx).GetAll()

	if err != nil {
		return nil, err
	}

	if len(existingRequests) > 0 {
		return nil, errors.New("join request already exists")
	}

	request := &models.JoinRequest{
		UserID:         userID,
		UserEmail:      user.Email,
		UserFirstName:  user.FirstName,
		UserLastName:   user.LastName,
		OrganizationID: organizationID,
		Status:         "pending",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	docRef, _, err := us.fs.Firestore.Collection("join_requests").Add(ctx, request)
	if err != nil {
		return nil, err
	}

	request.ID = docRef.ID
	return request, nil
}

func (us *UserService) GetUserJoinRequests(ctx context.Context, userID string) ([]*models.JoinRequest, error) {
	iter := us.fs.Firestore.Collection("join_requests").
		Where("user_id", "==", userID).
		Documents(ctx)
	defer iter.Stop()

	requests := make([]*models.JoinRequest, 0)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}

		var request models.JoinRequest
		if err := doc.DataTo(&request); err != nil {
			continue
		}

		// Only include pending requests for user-level queries
		if request.Status != "pending" {
			continue
		}

		request.ID = doc.Ref.ID
		requests = append(requests, &request)
	}

	return requests, nil
}

func (us *UserService) GetOrgJoinRequests(ctx context.Context, orgID string) ([]*models.JoinRequest, error) {
	iter := us.fs.Firestore.Collection("join_requests").
		Where("organization_id", "==", orgID).
		Documents(ctx)
	defer iter.Stop()

	requests := make([]*models.JoinRequest, 0)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}

		var request models.JoinRequest
		if err := doc.DataTo(&request); err != nil {
			continue
		}

		// Filter for pending status in application code
		if request.Status != "pending" {
			continue
		}

		// Check if user is already a member of the org
		user, err := us.GetUser(ctx, request.UserID)
		if err == nil && user.OrganizationID == orgID {
			// User is already a member, skip this join request
			continue
		}

		request.ID = doc.Ref.ID
		requests = append(requests, &request)
	}

	return requests, nil
}

func (us *UserService) ApproveJoinRequest(ctx context.Context, requestID, orgID string) error {
	doc, err := us.fs.Firestore.Collection("join_requests").Doc(requestID).Get(ctx)
	if err != nil {
		return err
	}

	var request models.JoinRequest
	if err := doc.DataTo(&request); err != nil {
		return err
	}

	if request.OrganizationID != orgID {
		return errors.New("permission denied: request does not belong to this organization")
	}

	if request.Status != "pending" {
		return errors.New("request has already been processed")
	}

	// Allow users to join multiple organizations: do not check OrganizationID

	// Add user to organization
	if err := us.AddUserToOrg(ctx, request.UserID, orgID, "member"); err != nil {
		return err
	}

	// Delete the processed request
	_, err = us.fs.Firestore.Collection("join_requests").Doc(requestID).Delete(ctx)
	if err != nil {
		return err
	}

	// Also delete any other pending requests for this user to ensure single-organization membership
	iter := us.fs.Firestore.Collection("join_requests").
		Where("user_id", "==", request.UserID).
		Where("status", "==", "pending").
		Documents(ctx)
	defer iter.Stop()

	for {
		d, e := iter.Next()
		if e != nil {
			break
		}
		// Skip the already-deleted requestID, but delete others
		if d.Ref.ID == requestID {
			continue
		}
		_, _ = d.Ref.Delete(ctx)
	}

	return err
}

func (us *UserService) RejectJoinRequest(ctx context.Context, requestID, orgID string) error {
	doc, err := us.fs.Firestore.Collection("join_requests").Doc(requestID).Get(ctx)
	if err != nil {
		return err
	}

	var request models.JoinRequest
	if err := doc.DataTo(&request); err != nil {
		return err
	}

	if request.OrganizationID != orgID {
		return errors.New("permission denied: request does not belong to this organization")
	}

	if request.Status != "pending" {
		return errors.New("request has already been processed")
	}

	// Delete the join request
	_, err = us.fs.Firestore.Collection("join_requests").Doc(requestID).Delete(ctx)

	return err
}

// CleanupJoinRequests deletes all legacy join requests that are not pending (approved/rejected)
// This helps clear out historical entries created before the new flow where processed requests are deleted.
func (us *UserService) CleanupJoinRequests(ctx context.Context) (int, error) {
	// Firestore supports 'in' operator for matching a set of values on a single field
	iter := us.fs.Firestore.Collection("join_requests").
		Where("status", "in", []interface{}{"approved", "rejected"}).
		Documents(ctx)
	defer iter.Stop()

	deleted := 0
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		_, delErr := doc.Ref.Delete(ctx)
		if delErr != nil {
			// If a delete fails, continue with others and return first error at the end
			// For simplicity, return the error immediately
			return deleted, delErr
		}
		deleted++
	}

	return deleted, nil
}
