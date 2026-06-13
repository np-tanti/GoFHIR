package gatekeeper

import (
	"context"
	"fmt"

	"github.com/pquerna/otp/totp"
)

// GenerateTOTPSecret creates a new TOTP secret for a user.
// Returns the base32 secret and a provisioning URI for QR codes.
func GenerateTOTPSecret(username string) (secret string, provisioningURI string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "GoFHIR",
		AccountName: username,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTP checks a TOTP code against the stored secret.
func ValidateTOTP(secret, code string) bool {
	return totp.Validate(secret, code)
}

// EnableTOTPForUser stores the TOTP secret for a user (caller handles encryption).
func (s *Store) EnableTOTP(ctx context.Context, userID, secret string) error {
	encrypted, err := s.encryptField(secret)
	if err != nil {
		return fmt.Errorf("encrypt totp: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE gatekeeper_users SET totp_secret = ? WHERE id = ?`,
		encrypted, userID,
	)
	return err
}

// DisableTOTPForUser clears the TOTP secret for a user.
func (s *Store) DisableTOTP(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE gatekeeper_users SET totp_secret = '' WHERE id = ?`,
		userID,
	)
	return err
}

// GetTOTPSecretForUser returns the decrypted TOTP secret for a user.
func (s *Store) GetTOTPSecretForUser(ctx context.Context, userID string) (string, error) {
	var encrypted string
	err := s.db.QueryRowContext(ctx,
		`SELECT totp_secret FROM gatekeeper_users WHERE id = ?`, userID,
	).Scan(&encrypted)
	if err != nil {
		return "", err
	}
	if encrypted == "" {
		return "", nil
	}
	return s.decryptField(encrypted)
}
