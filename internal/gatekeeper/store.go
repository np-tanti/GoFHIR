package gatekeeper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type StoredUser struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	TOTPSecret   string
	CreatedAt    time.Time
}

type StoredSession struct {
	ID        string
	UserID    string
	Role      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type StoredAPIKey struct {
	KeyHash   string
	UserID    string
	Role      string
	CreatedAt time.Time
	Revoked   bool
}

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("gatekeeper store open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("gatekeeper store migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS gatekeeper_users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'nurse',
		totp_secret TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS gatekeeper_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES gatekeeper_users(id),
		role TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		expires_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON gatekeeper_sessions(expires_at);
	CREATE TABLE IF NOT EXISTS gatekeeper_api_keys (
		key_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES gatekeeper_users(id),
		role TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		revoked INTEGER NOT NULL DEFAULT 0
	);
	PRAGMA journal_mode=WAL;
	PRAGMA synchronous=NORMAL;
	`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

func (s *Store) CreateUser(ctx context.Context, user *StoredUser) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO gatekeeper_users (id, username, password_hash, role, totp_secret, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.Role, user.TOTPSecret, user.CreatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (*StoredUser, error) {
	u := &StoredUser{}
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, totp_secret, created_at FROM gatekeeper_users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.TOTPSecret, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return u, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*StoredUser, error) {
	u := &StoredUser{}
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, totp_secret, created_at FROM gatekeeper_users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.TOTPSecret, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, session *StoredSession) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO gatekeeper_sessions (id, user_id, role, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.Role, session.CreatedAt.Format(time.RFC3339), session.ExpiresAt.Format(time.RFC3339))
	return err
}

func (s *Store) SessionByID(ctx context.Context, id string) (*StoredSession, error) {
	ses := &StoredSession{}
	var createdAt, expiresAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, role, created_at, expires_at FROM gatekeeper_sessions WHERE id = ?`, id).
		Scan(&ses.ID, &ses.UserID, &ses.Role, &createdAt, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ses.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	ses.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return ses, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM gatekeeper_sessions WHERE id = ?`, id)
	return err
}

func (s *Store) CreateAPIKey(ctx context.Context, key *StoredAPIKey) error {
	revoked := 0
	if key.Revoked {
		revoked = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO gatekeeper_api_keys (key_hash, user_id, role, created_at, revoked) VALUES (?, ?, ?, ?, ?)`,
		key.KeyHash, key.UserID, key.Role, key.CreatedAt.Format(time.RFC3339), revoked)
	return err
}

func (s *Store) APIKeyByHash(ctx context.Context, hash string) (*StoredAPIKey, error) {
	k := &StoredAPIKey{}
	var createdAt string
	var revoked int
	err := s.db.QueryRowContext(ctx,
		`SELECT key_hash, user_id, role, created_at, revoked FROM gatekeeper_api_keys WHERE key_hash = ?`, hash).
		Scan(&k.KeyHash, &k.UserID, &k.Role, &createdAt, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	k.Revoked = revoked == 1
	return k, nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM gatekeeper_sessions WHERE expires_at < strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`)
	return err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}