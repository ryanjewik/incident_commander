package services

import (
	"context"
	"errors"
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
	iter := us.fs.Firestore.Collection("users").
		Where("organization_id", "==", orgID).
		Documents(ctx)

	var users []*models.User
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}

		var user models.User
		if err := doc.DataTo(&user); err != nil {
			continue
		}
		user.ID = doc.Ref.ID
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
	_, err := us.fs.Firestore.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "organization_id", Value: orgID},
		{Path: "role", Value: role},
	})
	return err
}

func (us *UserService) RemoveUserFromOrg(ctx context.Context, userID string) error {
	_, err := us.fs.Firestore.Collection("users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "organization_id", Value: ""},
		{Path: "role", Value: ""},
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

func (us *UserService) CreateJoinRequest(ctx context.Context, userID, organizationID string) (*models.JoinRequest, error) {
	// Check if user already has an organization
	user, err := us.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if user.OrganizationID != "" && user.OrganizationID != "default" {
		return nil, errors.New("user already belongs to an organization")
	}

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

	// Add user to organization
	if err := us.AddUserToOrg(ctx, request.UserID, orgID, "member"); err != nil {
		return err
	}

	// Delete the join request
	_, err = us.fs.Firestore.Collection("join_requests").Doc(requestID).Delete(ctx)

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
