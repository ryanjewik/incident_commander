package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"os"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
)

// DecryptData unwraps the DEK using Cloud KMS and AES-GCM decrypts the
// provided base64-encoded ciphertexts (nonce||ciphertext). It returns
// the plaintext API and App keys (empty string if input was empty).
func DecryptData(encryptedAPI string, encryptedApp string, wrappedDEK string) (string, string, error) {
	if wrappedDEK == "" {
		return "", "", errors.New("no wrapped DEK provided")
	}

	dekBytes, err := base64.StdEncoding.DecodeString(wrappedDEK)
	if err != nil {
		return "", "", err
	}

	ctx := context.Background()
	kmsClient, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return "", "", err
	}
	defer kmsClient.Close()

	keyName := os.Getenv("KMS_CRYPTO_KEY")
	if keyName == "" {
		keyName = "projects/incident-response/locations/global/keyRings/hackathon_key_ring/cryptoKeys/hackathon_key"
	}

	resp, err := kmsClient.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:       keyName,
		Ciphertext: dekBytes,
	})
	if err != nil {
		return "", "", err
	}

	dek := resp.Plaintext

	var apiPlain, appPlain string

	if encryptedAPI != "" {
		p, err := decryptAESGCM(dek, encryptedAPI)
		if err != nil {
			return "", "", err
		}
		apiPlain = p
	}

	if encryptedApp != "" {
		p, err := decryptAESGCM(dek, encryptedApp)
		if err != nil {
			return "", "", err
		}
		appPlain = p
	}

	return apiPlain, appPlain, nil
}

// decryptAESGCM expects base64(nonce || ciphertext) and returns plaintext.
func decryptAESGCM(key []byte, b64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("malformed ciphertext")
	}
	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	pt, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
