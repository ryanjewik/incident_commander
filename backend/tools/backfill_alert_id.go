package main

import (
	"context"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"github.com/ryanjewik/incident_commander/backend/services"
	"google.golang.org/api/iterator"
)

func main() {
	creds := os.Getenv("FIREBASE_CREDENTIALS_PATH")
	if creds == "" {
		creds = "./firebase-service-account.json"
	}

	svc, err := services.NewFirebaseService(creds)
	if err != nil {
		log.Fatalf("failed to init firebase: %v", err)
	}
	defer svc.Close()

	ctx := context.Background()
	iter := svc.Firestore.Collection("incidents").Documents(ctx)

	var updated, scanned int
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("failed iterating incidents: %v", err)
		}
		scanned++

		data := doc.Data()

		// if alert_id already present and non-empty, skip
		if v, ok := data["alert_id"]; ok {
			if s, ok2 := v.(string); ok2 && s != "" {
				continue
			}
		}

		// attempt to read metadata.monitor.alert_id
		var alertID string
		if meta, ok := data["metadata"].(map[string]interface{}); ok {
			if mon, ok2 := meta["monitor"].(map[string]interface{}); ok2 {
				if a, ok3 := mon["alert_id"].(string); ok3 && a != "" {
					alertID = a
				} else if a2, ok4 := mon["alert_id"]; ok4 {
					alertID = fmtString(a2)
				}
			}
		}

		if alertID == "" {
			continue
		}

		_, err = svc.Firestore.Collection("incidents").Doc(doc.Ref.ID).Update(ctx, []firestore.Update{
			{Path: "alert_id", Value: alertID},
			{Path: "updated_at", Value: firestore.ServerTimestamp},
		})
		if err != nil {
			log.Printf("failed to update doc %s: %v", doc.Ref.ID, err)
			continue
		}
		updated++
		log.Printf("updated doc %s with alert_id=%s", doc.Ref.ID, alertID)
	}

	log.Printf("scanned=%d updated=%d", scanned, updated)
}

func fmtString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
