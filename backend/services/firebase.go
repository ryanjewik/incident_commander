package services

import (
	"context"
	"log"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

type FirebaseService struct {
	App       *firebase.App
	Auth      *auth.Client
	Firestore *firestore.Client
}

func NewFirebaseService(credentialsPath string) (*FirebaseService, error) {
	ctx := context.Background()

	opt := option.WithCredentialsFile(credentialsPath)
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return nil, err
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, err
	}

	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, err
	}

	log.Println("Firebase initialized successfully")

	return &FirebaseService{
		App:       app,
		Auth:      authClient,
		Firestore: firestoreClient,
	}, nil
}

func (fs *FirebaseService) Close() error {
	return fs.Firestore.Close()
}