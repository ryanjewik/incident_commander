package services

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// HashWebhookSecret will hash the provided webhook secret.
// TODO: implement proper hashing (e.g., HMAC/SHA256 with salt or KMS-backed hashing)
// For now this is a placeholder that returns the input unchanged to keep
// behavior predictable while the real implementation is being developed.
func HashWebhookSecret(secret string) (string, error) {
	if secret == "" {
		return "", errors.New("empty secret")
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckSecretHash(secret, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret))
	return err == nil
}
