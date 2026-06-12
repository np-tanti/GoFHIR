package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	secretsFile := filepath.Join(tmpDir, "secrets.enc")
	masterKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	err := InitSecrets(secretsFile, masterKeyHex)
	if err != nil {
		t.Fatalf("InitSecrets failed: %v", err)
	}

	mgr, err := NewManager(secretsFile)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = mgr.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
}

func TestSetAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	secretsFile := filepath.Join(tmpDir, "secrets.enc")
	masterKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	err := InitSecrets(secretsFile, masterKeyHex)
	if err != nil {
		t.Fatalf("InitSecrets failed: %v", err)
	}

	mgr, err := NewManager(secretsFile)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = mgr.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	testKey := "GOFHIR_TEST_KEY"
	testValue := "test-secret-value"

	err = mgr.Set(testKey, testValue)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	retrieved, err := mgr.Get(testKey)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved != testValue {
		t.Errorf("expected %q, got %q", testValue, retrieved)
	}
}

func TestRotate(t *testing.T) {
	tmpDir := t.TempDir()
	secretsFile := filepath.Join(tmpDir, "secrets.enc")
	masterKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	err := InitSecrets(secretsFile, masterKeyHex)
	if err != nil {
		t.Fatalf("InitSecrets failed: %v", err)
	}

	mgr, err := NewManager(secretsFile)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = mgr.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	err = mgr.Rotate("GOFHIR_AUDIT_HMAC_KEY")
	if err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	value, err := mgr.Get("GOFHIR_AUDIT_HMAC_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(value) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(value))
	}
}

func TestEncryptDecrypt(t *testing.T) {
	masterKey := make([]byte, 32)
	plaintext := []byte("test-secret-value")

	ciphertext, err := encrypt(masterKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := decrypt(masterKey, ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestInvalidMasterKey(t *testing.T) {
	os.Unsetenv("GOFHIR_MASTER_KEY")
	secretsFile := filepath.Join(t.TempDir(), "nonexistent.enc")

	_, err := NewManager(secretsFile)
	if err == nil {
		t.Error("expected error for missing master key")
	}
}
