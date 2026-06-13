package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Resource struct {
	ID           string
	ResourceType string
	Version      int
	Data         []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Deleted      bool
}

type SearchResult struct {
	Resources []*Resource
	Total     int
	Offset    int
	Count     int
	HasMore   bool
}

type SearchFilters struct {
	ID            string
	LastUpdated   string
	Name          string
	Code          string
	Subject       string
	PatientIDs    []string
	Count         int
	Offset        int
	MaxCount      int
	DefaultCount  int
	DefaultOffset int
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
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
		`CREATE TABLE IF NOT EXISTS fhir_resources (
			id TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			version INTEGER NOT NULL,
			data JSON NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			deleted INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fhir_type_id ON fhir_resources(resource_type, id)`,
		`CREATE INDEX IF NOT EXISTS idx_fhir_id_version ON fhir_resources(id, version)`,
		`CREATE INDEX IF NOT EXISTS idx_fhir_updated ON fhir_resources(updated_at)`,
	}
	for _, stmt := range ddl {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

type txKey struct{}

func withTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func txFromCtx(ctx context.Context) *sql.Tx {
	if t, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return t
	}
	return nil
}

func (s *Store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(withTx(ctx, tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx := txFromCtx(ctx); tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	return s.db.ExecContext(ctx, query, args...)
}

func (s *Store) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	if tx := txFromCtx(ctx); tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return s.db.QueryRowContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if tx := txFromCtx(ctx); tx != nil {
		return tx.QueryContext(ctx, query, args...)
	}
	return s.db.QueryContext(ctx, query, args...)
}

func (r *Resource) scanRow(row *sql.Row) error {
	return row.Scan(&r.ID, &r.ResourceType, &r.Version, &r.Data, &r.CreatedAt, &r.UpdatedAt, &r.Deleted)
}

func (r *Resource) scanRows(rows *sql.Rows) error {
	return rows.Scan(&r.ID, &r.ResourceType, &r.Version, &r.Data, &r.CreatedAt, &r.UpdatedAt, &r.Deleted)
}

func parseResourceType(data []byte) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	rt, ok := m["resourceType"].(string)
	if !ok {
		return "", errors.New("missing resourceType")
	}
	return strings.ToLower(rt), nil
}

func extractJSONField(data []byte, field string) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	v, ok := m[field]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%d", int64(val))
	default:
		s, _ := json.Marshal(val)
		return string(s)
	}
}

func (s *Store) Create(ctx context.Context, r *Resource) (*Resource, error) {
	if r.ID == "" {
		return nil, errors.New("id is required")
	}
	if r.Data == nil {
		return nil, errors.New("data is required")
	}
	rt, err := parseResourceType(r.Data)
	if err != nil {
		return nil, err
	}
	if rt == "" {
		return nil, errors.New("resourceType is required in data")
	}
	r.ResourceType = rt
	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.Version = 1
	r.Deleted = false
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO fhir_resources (id, resource_type, version, data, created_at, updated_at, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.ResourceType, r.Version, string(r.Data), r.CreatedAt, r.UpdatedAt, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	return r, nil
}

func (s *Store) Update(ctx context.Context, r *Resource) (*Resource, error) {
	if r.ID == "" {
		return nil, errors.New("id is required")
	}
	if r.Data == nil {
		return nil, errors.New("data is required")
	}
	rt, err := parseResourceType(r.Data)
	if err != nil {
		return nil, err
	}
	if rt == "" {
		return nil, errors.New("resourceType is required in data")
	}
	row := s.queryRow(ctx,
		"SELECT version FROM fhir_resources WHERE id = ? ORDER BY version DESC LIMIT 1",
		r.ID,
	)
	var lastVer int
	if err := row.Scan(&lastVer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resource %q not found", r.ID)
		}
		return nil, fmt.Errorf("lookup: %w", err)
	}
	now := time.Now().UTC()
	r.Version = lastVer + 1
	r.ResourceType = rt
	r.CreatedAt = now
	r.UpdatedAt = now
	r.Deleted = false
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO fhir_resources (id, resource_type, version, data, created_at, updated_at, deleted) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.ID, r.ResourceType, r.Version, string(r.Data), r.CreatedAt, r.UpdatedAt, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	return r, nil
}

func (s *Store) SoftDelete(ctx context.Context, id string) error {
	row := s.queryRow(ctx,
		"SELECT version FROM fhir_resources WHERE id = ? ORDER BY version DESC LIMIT 1",
		id,
	)
	var lastVer int
	if err := row.Scan(&lastVer); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("resource %q not found", id)
		}
		return err
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO fhir_resources (id, resource_type, version, data, created_at, updated_at, deleted) SELECT id, resource_type, ?, ?, ?, ?, 1 FROM fhir_resources WHERE id = ? AND version = ? ORDER BY id, version LIMIT 1",
		lastVer+1, now, now, now, id, lastVer,
	)
	if err != nil {
		return fmt.Errorf("soft-delete: %w", err)
	}
	return nil
}

func (s *Store) Read(ctx context.Context, id string) (*Resource, error) {
	row := s.queryRow(ctx,
		`SELECT id, resource_type, version, data, created_at, updated_at, deleted
		 FROM fhir_resources
		 WHERE id = ? AND deleted = 0
		 ORDER BY version DESC LIMIT 1`,
		id,
	)
	var r Resource
	if err := r.scanRow(row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resource %q not found", id)
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) ReadVersion(ctx context.Context, id string, version int) (*Resource, error) {
	row := s.queryRow(ctx,
		`SELECT id, resource_type, version, data, created_at, updated_at, deleted
		 FROM fhir_resources
		 WHERE id = ? AND version = ? AND deleted = 0`,
		id, version,
	)
	var r Resource
	if err := r.scanRow(row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resource %q version %d not found", id, version)
		}
		return nil, err
	}
	return &r, nil
}

func (s *Store) History(ctx context.Context, id string) ([]*Resource, error) {
	rows, err := s.query(ctx,
		`SELECT id, resource_type, version, data, created_at, updated_at, deleted
		 FROM fhir_resources
		 WHERE id = ?
		 ORDER BY version DESC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Resource
	for rows.Next() {
		var r Resource
		if err := r.scanRows(rows); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) HistoryAll(ctx context.Context, f SearchFilters) (*SearchResult, error) {
	count := f.Count
	if count <= 0 {
		count = 0
	}
	if f.MaxCount > 0 && count > f.MaxCount {
		count = f.MaxCount
	}
	var where []string
	var args []any
	where = append(where, "deleted = 0")
	if f.LastUpdated != "" {
		where = append(where, "updated_at >= ?")
		args = append(args, f.LastUpdated)
	}
	whereStr := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fhir_resources WHERE "+whereStr, args...,
	).Scan(&total); err != nil {
		return nil, err
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, count, offset)
	query := fmt.Sprintf(
		"SELECT id, resource_type, version, data, created_at, updated_at, deleted FROM fhir_resources WHERE %s ORDER BY updated_at DESC LIMIT ? OFFSET ?",
		whereStr,
	)
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Resource
	for rows.Next() {
		var r Resource
		if err := r.scanRows(rows); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	hasMore := offset+count < total
	return &SearchResult{Resources: out, Total: total, Offset: offset, Count: len(out), HasMore: hasMore}, nil
}

func (s *Store) Search(ctx context.Context, resourceType string, f SearchFilters) (*SearchResult, error) {
	if resourceType == "" {
		return nil, errors.New("resource_type is required")
	}
	count := f.Count
	if count <= 0 {
		count = 0
	}
	if f.MaxCount > 0 && count > f.MaxCount {
		count = f.MaxCount
	}
	if count == 0 {
		count = f.DefaultCount
		if count == 0 {
			count = 20
		}
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	var where []string
	var args []any
	where = append(where, "resource_type = ? AND deleted = 0")
	args = append(args, resourceType)
	if f.ID != "" {
		where = append(where, "id = ?")
		args = append(args, f.ID)
	}
	if f.LastUpdated != "" {
		where = append(where, "updated_at >= ?")
		args = append(args, f.LastUpdated)
	}
	if f.Name != "" {
		where = append(where, "(json_extract(data, '$.name') LIKE ? ESCAPE '\\' OR id LIKE ? ESCAPE '\\')")
		args = append(args, "%"+f.Name+"%", "%"+f.Name+"%")
	}
	if f.Code != "" {
		where = append(where, "json_extract(data, '$.code') LIKE ? ESCAPE '\\'")
		args = append(args, "%"+f.Code+"%")
	}
	if f.Subject != "" {
		where = append(where, "json_extract(data, '$.subject.reference') = ? OR json_extract(data, '$.subject.display') LIKE ? ESCAPE '\\'")
		args = append(args, f.Subject, "%"+f.Subject+"%")
	}
	if len(f.PatientIDs) > 0 && resourceType == "patient" {
		placeholders := make([]string, len(f.PatientIDs))
		for i, id := range f.PatientIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, "id IN ("+strings.Join(placeholders, ",")+")")
	}
	whereStr := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fhir_resources WHERE "+whereStr, args...,
	).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, count, offset)
	query := fmt.Sprintf(
		"SELECT id, resource_type, version, data, created_at, updated_at, deleted FROM fhir_resources WHERE %s ORDER BY updated_at DESC LIMIT ? OFFSET ?",
		whereStr,
	)
	rows, err := s.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Resource
	for rows.Next() {
		var r Resource
		if err := r.scanRows(rows); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	hasMore := offset+count < total
	return &SearchResult{Resources: out, Total: total, Offset: offset, Count: len(out), HasMore: hasMore}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
