package services

import (
	"context"
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

func (us *UserService) CreateUser(ctx context.Context, email, password, displayName string) (*models.User, error) {
	params := (&auth.UserToCreate{}).
		Email(email).
		Password(password).
		DisplayName(displayName)

	authUser, err := us.fs.Auth.CreateUser(ctx, params)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		ID:             authUser.UID,
		Email:          email,
		DisplayName:    displayName,
		OrganizationID: "default", // Set default organization instead of empty string
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

func (us *UserService) CreateOrganization(ctx context.Context, name, creatorID string) (*models.Organization, error) {
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