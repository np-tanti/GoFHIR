package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (m *Manager) Rotate(key string) error {
	if m.secretsFile == nil {
		return fmt.Errorf("secrets not loaded")
	}

	newValue, err := generateSecretValue(key)
	if err != nil {
		return fmt.Errorf("generate secret: %w", err)
	}

	if err := m.createBackup(); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	return m.Set(key, newValue)
}

func (m *Manager) RotateAll() error {
	if m.secretsFile == nil {
		return fmt.Errorf("secrets not loaded")
	}

	if err := m.createBackup(); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	for key := range m.secretsFile.Secrets {
		newValue, err := generateSecretValue(key)
		if err != nil {
			return fmt.Errorf("generate secret %s: %w", key, err)
		}
		if err := m.Set(key, newValue); err != nil {
			return fmt.Errorf("set secret %s: %w", key, err)
		}
	}

	return nil
}

func generateSecretValue(key string) (string, error) {
	switch key {
	case "GOFHIR_AUDIT_HMAC_KEY":
		return generateHexBytes(32)
	case "GOFHIR_JWT_SECRET":
		return generateHexBytes(32)
	default:
		return generateHexBytes(32)
	}
}

func generateHexBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (m *Manager) createBackup() error {
	backupPath := m.filePath + ".bak"

	if _, err := os.Stat(m.filePath); os.IsNotExist(err) {
		return nil
	}

	dir := filepath.Dir(backupPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	src, err := os.Open(m.filePath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func (m *Manager) RestoreBackup() error {
	backupPath := m.filePath + ".bak"

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	if err := os.WriteFile(m.filePath, data, 0600); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}

	return m.Load()
}
