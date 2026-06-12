package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultSecretsFile = "~/.gofhir/secrets.enc"
	MasterKeyFile      = "~/.gofhir/master.key"
	FileVersion        = 1
)

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func NewManager(filePath string) (*Manager, error) {
	if filePath == "" {
		filePath = DefaultSecretsFile
	}
	filePath = expandPath(filePath)

	masterKey, err := loadMasterKey()
	if err != nil {
		return nil, fmt.Errorf("load master key: %w", err)
	}

	return &Manager{
		filePath:  filePath,
		masterKey: masterKey,
	}, nil
}

func loadMasterKey() ([]byte, error) {
	if keyHex := os.Getenv("GOFHIR_MASTER_KEY"); keyHex != "" {
		return parseMasterKey(keyHex)
	}

	keyPath := expandPath(MasterKeyFile)
	if data, err := os.ReadFile(keyPath); err == nil {
		keyHex := strings.TrimSpace(string(data))
		return parseMasterKey(keyHex)
	}

	return nil, fmt.Errorf("master key not found: set GOFHIR_MASTER_KEY or create %s", MasterKeyFile)
}

func parseMasterKey(keyHex string) ([]byte, error) {
	keyHex = strings.TrimSpace(keyHex)
	if len(keyHex) != 64 {
		return nil, ErrInvalidMasterKey
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, ErrInvalidMasterKey
	}

	return key, nil
}

func InitSecrets(filePath, masterKeyHex string) error {
	filePath = expandPath(filePath)

	if masterKeyHex == "" {
		var err error
		masterKeyHex, err = generateMasterKeyHex()
		if err != nil {
			return fmt.Errorf("generate master key: %w", err)
		}
	}

	masterKey, err := parseMasterKey(masterKeyHex)
	if err != nil {
		return err
	}

	if err := saveMasterKey(masterKeyHex); err != nil {
		return err
	}

	mgr := &Manager{
		filePath:  filePath,
		masterKey: masterKey,
		secretsFile: &SecretsFile{
			Version:   FileVersion,
			CreatedAt: now(),
			Secrets:   make(map[string]SecretEntry),
			Metadata:  make(map[string]string),
		},
	}

	return mgr.Save()
}

func generateMasterKeyHex() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

func saveMasterKey(keyHex string) error {
	keyPath := expandPath(MasterKeyFile)
	dir := filepath.Dir(keyPath)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create master key directory: %w", err)
	}

	if err := os.WriteFile(keyPath, []byte(keyHex), 0600); err != nil {
		return fmt.Errorf("write master key file: %w", err)
	}

	return nil
}

func (m *Manager) Load() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSecretsFileNotFound
		}
		return fmt.Errorf("read secrets file: %w", err)
	}

	plaintext, err := decrypt(m.masterKey, data)
	if err != nil {
		return err
	}
	defer zeroBytes(plaintext)

	var sf SecretsFile
	if err := json.Unmarshal(plaintext, &sf); err != nil {
		return ErrCorruptFile
	}

	m.secretsFile = &sf
	return nil
}

func (m *Manager) Save() error {
	if m.secretsFile == nil {
		return fmt.Errorf("secrets file not initialized")
	}

	plaintext, err := json.MarshalIndent(m.secretsFile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}
	defer zeroBytes(plaintext)

	ciphertext, err := encrypt(m.masterKey, plaintext)
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create secrets directory: %w", err)
	}

	if err := os.WriteFile(m.filePath, ciphertext, 0600); err != nil {
		return fmt.Errorf("write secrets file: %w", err)
	}

	return nil
}

func (m *Manager) Get(key string) (string, error) {
	if m.secretsFile == nil {
		return "", fmt.Errorf("secrets not loaded")
	}

	entry, ok := m.secretsFile.Secrets[key]
	if !ok {
		return "", ErrSecretNotFound
	}

	plaintext, err := decryptFromBase64(m.masterKey, entry.Value)
	if err != nil {
		return "", err
	}
	defer zeroBytes(plaintext)

	return string(plaintext), nil
}

func (m *Manager) Set(key, value string) error {
	if m.secretsFile == nil {
		return fmt.Errorf("secrets not initialized")
	}

	encrypted, err := encryptToBase64(m.masterKey, []byte(value))
	if err != nil {
		return err
	}

	entry := SecretEntry{
		Value:     encrypted,
		Version:   1,
		CreatedAt: now(),
	}

	if existing, ok := m.secretsFile.Secrets[key]; ok {
		entry.Version = existing.Version + 1
		t := now()
		entry.RotatedAt = &t
	}

	m.secretsFile.Secrets[key] = entry
	return m.Save()
}

func (m *Manager) ToList() map[string]string {
	if m.secretsFile == nil {
		return nil
	}

	result := make(map[string]string)
	for key, entry := range m.secretsFile.Secrets {
		if len(entry.Value) > 0 {
			result[key] = maskValue(key)
		}
	}
	return result
}

func maskValue(key string) string {
	return "********"
}

func (m *Manager) Validate() error {
	required := []string{
		"GOFHIR_AUDIT_HMAC_KEY",
		"GOFHIR_JWT_SECRET",
	}

	var missing []string
	for _, key := range required {
		if _, err := m.Get(key); err != nil {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required secrets: %v", missing)
	}
	return nil
}

func (m *Manager) ExportToEnv() (map[string]string, error) {
	if m.secretsFile == nil {
		return nil, fmt.Errorf("secrets not loaded")
	}

	result := make(map[string]string)
	for key := range m.secretsFile.Secrets {
		value, err := m.Get(key)
		if err != nil {
			return nil, fmt.Errorf("get secret %s: %w", key, err)
		}
		result[key] = value
	}

	return result, nil
}

func now() time.Time {
	return time.Now().UTC()
}

func (m *Manager) FilePath() string {
	return m.filePath
}
