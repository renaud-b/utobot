package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const EncryptionKeySize = 32

func GenerateEncryptionKey() ([]byte, error) {
	key := make([]byte, EncryptionKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	return key, nil
}

func EncodeEncryptionKey(key []byte) (string, error) {
	if len(key) != EncryptionKeySize {
		return "", fmt.Errorf("encryption key must be %d bytes", EncryptionKeySize)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func DecodeEncryptionKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != EncryptionKeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes", EncryptionKeySize)
	}
	return key, nil
}

func EncryptAES256GCM(key []byte, plaintext string) (string, error) {
	if len(key) != EncryptionKeySize {
		return "", fmt.Errorf("encryption key must be %d bytes", EncryptionKeySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM cipher: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func DecryptAES256GCM(key []byte, encodedPayload string) (string, error) {
	if len(key) != EncryptionKeySize {
		return "", fmt.Errorf("encryption key must be %d bytes", EncryptionKeySize)
	}

	payload, err := base64.StdEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", fmt.Errorf("decode encrypted payload: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM cipher: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", fmt.Errorf("encrypted payload is too short")
	}

	nonce := payload[:nonceSize]
	ciphertext := payload[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt payload: %w", err)
	}

	return string(plaintext), nil
}
