package auditor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("auditor open: %w", err)
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("auditor pragma: %w", err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS audit_log (
			seq INTEGER PRIMARY KEY,
			prev_hash BLOB NOT NULL,
			timestamp INTEGER NOT NULL,
			action TEXT NOT NULL,
			actor_id TEXT NOT NULL DEFAULT '',
			session_id TEXT NOT NULL DEFAULT '',
			payload BLOB NOT NULL DEFAULT '',
			hmac BLOB NOT NULL
		)`,
		`CREATE TRIGGER IF NOT EXISTS audit_log_append_only
		 BEFORE UPDATE ON audit_log
		 BEGIN
		 	SELECT RAISE(ABORT, 'audit log is append-only');
		 END`,
		`CREATE TRIGGER IF NOT EXISTS audit_log_no_delete
		 BEFORE DELETE ON audit_log
		 BEGIN
		 	SELECT RAISE(ABORT, 'audit log is append-only');
		 END`,
	}
	for _, stmt := range ddl {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("auditor migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Append(ctx context.Context, e *Entry) error {
	payload := e.Payload
	if payload == nil {
		payload = []byte{}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (seq, prev_hash, timestamp, action, actor_id, session_id, payload, hmac) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Seq, e.PrevHash[:], e.Timestamp, e.Action, e.ActorID, e.SessionID, payload, e.HMAC[:],
	)
	if err != nil {
		return fmt.Errorf("auditor append: %w", err)
	}
	return nil
}

func (s *Store) ReadRange(ctx context.Context, seqFrom, seqTo uint64) ([]Entry, error) {
	if seqTo < seqFrom {
		return nil, errors.New("seqTo must be >= seqFrom")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, prev_hash, timestamp, action, actor_id, session_id, payload, hmac FROM audit_log WHERE seq >= ? AND seq <= ? ORDER BY seq ASC`,
		seqFrom, seqTo,
	)
	if err != nil {
		return nil, fmt.Errorf("auditor read range: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *Store) ReadAll(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, prev_hash, timestamp, action, actor_id, session_id, payload, hmac FROM audit_log ORDER BY seq ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("auditor read all: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *Store) EntryBySeq(ctx context.Context, seq uint64) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT seq, prev_hash, timestamp, action, actor_id, session_id, payload, hmac FROM audit_log WHERE seq = ?`,
		seq,
	)
	var e Entry
	if err := scanEntry(row, &e); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("entry %d not found", seq)
		}
		return nil, err
	}
	return &e, nil
}

func (s *Store) LastSeq(ctx context.Context) (uint64, error) {
	var seq uint64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM audit_log`).Scan(&seq)
	return seq, err
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&n)
	return n, err
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func GenerateHMACKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate hmac key: %w", err)
	}
	return key, nil
}

func HMACKeyHex(key []byte) string {
	return hex.EncodeToString(key)
}

func HMACKeyFromHex(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

func FirstEntry(action, actorID, sessionID string, payload []byte, hmacKey []byte) Entry {
	var zero [32]byte
	now := time.Now().UnixNano()
	e := Entry{
		Seq:       1,
		PrevHash:  zero,
		Timestamp: now,
		Action:    action,
		ActorID:   actorID,
		SessionID: sessionID,
		Payload:   payload,
	}
	e.HMAC = e.ComputeHMAC(hmacKey)
	return e
}

func NextEntry(prev Entry, action, actorID, sessionID string, payload []byte, hmacKey []byte) Entry {
	return NewEntry(HashOf(&prev), prev.Seq+1, action, actorID, sessionID, payload, hmacKey)
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var e Entry
		var prevHash, hmac []byte
		if err := rows.Scan(&e.Seq, &prevHash, &e.Timestamp, &e.Action, &e.ActorID, &e.SessionID, &e.Payload, &hmac); err != nil {
			return nil, err
		}
		copy(e.PrevHash[:], prevHash)
		copy(e.HMAC[:], hmac)
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEntry(row *sql.Row, e *Entry) error {
	var prevHash, hmac []byte
	if err := row.Scan(&e.Seq, &prevHash, &e.Timestamp, &e.Action, &e.ActorID, &e.SessionID, &e.Payload, &hmac); err != nil {
		return err
	}
	copy(e.PrevHash[:], prevHash)
	copy(e.HMAC[:], hmac)
	return nil
}
