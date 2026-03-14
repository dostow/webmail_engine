package crypto

import (
	"testing"
)

func TestEncryptor_EncryptDecrypt(t *testing.T) {
	encryptor, err := NewEncryptor("test-key-32-bytes-long-exactly!!")
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	plaintext := "secret-password-123"

	// Encrypt
	ciphertext, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Ciphertext should be different each time (random nonce)
	ciphertext2, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if ciphertext == ciphertext2 {
		t.Error("Expected different ciphertexts for same plaintext (random nonce)")
	}

	// Decrypt
	decrypted, err := encryptor.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptor_DifferentKeys(t *testing.T) {
	encryptor1, err := NewEncryptor("key1-32-bytes-long-exact-size!!!")
	if err != nil {
		t.Fatalf("Failed to create encryptor1: %v", err)
	}
	encryptor2, err := NewEncryptor("key2-32-bytes-long-exact-size!!!")
	if err != nil {
		t.Fatalf("Failed to create encryptor2: %v", err)
	}

	plaintext := "secret-data"

	ciphertext, err := encryptor1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypting with wrong key should fail
	_, err = encryptor2.Decrypt(ciphertext)
	if err == nil {
		t.Error("Expected error when decrypting with wrong key")
	}
}

func TestEncryptor_TamperedCiphertext(t *testing.T) {
	encryptor, _ := NewEncryptor("test-key-32-bytes-long-exactly!!")

	plaintext := "important-data"
	ciphertext, _ := encryptor.Encrypt(plaintext)

	// Tamper with the ciphertext
	tampered := ciphertext[:len(ciphertext)-1] + "X"

	_, err := encryptor.Decrypt(tampered)
	if err == nil {
		t.Error("Expected error when decrypting tampered ciphertext")
	}
}

func TestParseEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid 32-byte key", "exactly-32-bytes-long-key-here!!", false},
		{"empty key (uses default)", "", false},
		{"too short", "short", true},
		{"too long", "this-key-is-way-too-long-for-aes-key-exactly", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEncryptionKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEncryptionKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncryptor_NonceUniqueness(t *testing.T) {
	encryptor, _ := NewEncryptor("test-key-32-bytes-long-exactly!!")

	// Encrypt the same plaintext many times
	seen := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		ciphertext, err := encryptor.Encrypt("test-plaintext")
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		if seen[ciphertext] {
			t.Errorf("Duplicate ciphertext detected at iteration %d - nonce reuse!", i)
		}
		seen[ciphertext] = true
	}
}
