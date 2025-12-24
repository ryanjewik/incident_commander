package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"

	kms "cloud.google.com/go/kms/apiv1"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
)

// EncryptData generates a random DEK, wraps it using Cloud KMS, and
// uses the DEK to AES-GCM encrypt the provided api and app secrets.
// It returns base64-encoded ciphertexts for api, app and the wrapped DEK.
func EncryptData(api string, app string) (string, string, string, error) {
	if api == "" && app == "" {
		return "", "", "", errors.New("no data to encrypt")
	}

	dek := make([]byte, 32) // 256-bit key
	if _, err := rand.Read(dek); err != nil {
		return "", "", "", err
	}

	ctx := context.Background()
	kmsClient, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return "", "", "", err
	}
	defer kmsClient.Close()

	keyName := os.Getenv("KMS_CRYPTO_KEY")

	req := &kmspb.EncryptRequest{
		Name:      keyName,
		Plaintext: dek,
	}
	resp, err := kmsClient.Encrypt(ctx, req)
	if err != nil {
		return "", "", "", err
	}

	encryptedDEK := resp.Ciphertext
	encryptedDEKStr := base64.StdEncoding.EncodeToString(encryptedDEK)

	var encryptedAPI, encryptedAPP string

	if api != "" {
		encryptedAPI, err = encryptAESGCM(dek, []byte(api))
		if err != nil {
			return "", "", "", err
		}
	}

	if app != "" {
		encryptedAPP, err = encryptAESGCM(dek, []byte(app))
		if err != nil {
			return "", "", "", err
		}
	}

	return encryptedAPI, encryptedAPP, encryptedDEKStr, nil
}

// encryptAESGCM encrypts plaintext with the provided key using AES-GCM.
// The returned string is base64(nonce || ciphertext).
func encryptAESGCM(key []byte, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	out := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}
