package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Encryptor handles XChaCha20-Poly1305 encryption and decryption
type Encryptor struct {
	key []byte
}

// NewEncryptor creates a new encryptor with the given key
// Key must be exactly 32 bytes (256 bits) for XChaCha20-Poly1305
// Key can be provided as raw 32-byte string or hex-encoded (64 chars)
func NewEncryptor(key string) (*Encryptor, error) {
	keyBytes, err := ParseEncryptionKey(key)
	if err != nil {
		return nil, err
	}
	return &Encryptor{key: keyBytes}, nil
}

// Encrypt encrypts plaintext using XChaCha20-Poly1305
// Uses a 192-bit random nonce for each encryption, making nonce reuse
// practically impossible even with billions of encryptions
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	aead, err := chacha20poly1305.NewX(e.key)
	if err != nil {
		return "", err
	}

	// XChaCha20-Poly1305 uses 192-bit (24 byte) nonce
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal prepends the nonce to the ciphertext
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using XChaCha20-Poly1305
// Expects the nonce to be prepended to the ciphertext
func (e *Encryptor) Decrypt(ciphertextStr string) (string, error) {
	if e == nil || e.key == nil {
		return "", errors.New("encryptor not initialized: encryption key is nil")
	}

	aead, err := chacha20poly1305.NewX(e.key)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return "", err
	}

	nonceSize := chacha20poly1305.NonceSizeX
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// ParseEncryptionKey parses and validates an encryption key
// XChaCha20-Poly1305 requires exactly 32 bytes (256 bits)
// Key can be provided as raw 32-byte string or hex-encoded (64 hex chars)
func ParseEncryptionKey(key string) ([]byte, error) {
	if key == "" {
		// Use default key (in production, this should be configured via environment)
		key = "default-encryption-key-32-bytes!"
	}

	// If key is 64 hex characters, decode it to 32 bytes
	if len(key) == 64 {
		keyBytes, err := hex.DecodeString(key)
		if err == nil && len(keyBytes) == 32 {
			return keyBytes, nil
		}
	}

	// Otherwise, expect raw 32-byte key
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (256 bits) for XChaCha20-Poly1305, got %d bytes", len(key))
	}

	return []byte(key), nil
}
