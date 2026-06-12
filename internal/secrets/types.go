package secrets

import (
	"fmt"
	"time"
)

type SecretEntry struct {
	Value     string     `json:"value"`
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	RotatedAt *time.Time `json:"rotated_at,omitempty"`
}

type SecretsFile struct {
	Version   int                  `json:"version"`
	CreatedAt time.Time            `json:"created_at"`
	Secrets   map[string]SecretEntry `json:"secrets"`
	Metadata  map[string]string    `json:"metadata"`
}

type Manager struct {
	filePath    string
	masterKey   []byte
	secretsFile *SecretsFile
}

var (
	ErrDecryptionFailed    = fmt.Errorf("decryption failed: invalid master key or corrupt file")
	ErrSecretsFileNotFound = fmt.Errorf("secrets file not found")
	ErrSecretNotFound      = fmt.Errorf("secret not found")
	ErrInvalidMasterKey    = fmt.Errorf("invalid master key: must be 64 hex characters (32 bytes)")
	ErrCorruptFile         = fmt.Errorf("corrupt secrets file")
)
