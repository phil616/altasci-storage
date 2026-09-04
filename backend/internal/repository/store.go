package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func NowMS() int64 { return time.Now().UTC().UnixMilli() }

func NormalizeEmail(email string) string {
	return strings.ToLower(norm.NFC.String(strings.TrimSpace(email)))
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "unique constraint") || strings.Contains(s, "constraint failed") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) Setting(ctx context.Context, key string, dst any) error {
	var raw string
	if err := s.DB.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key = ?", key).Scan(&raw); err != nil {
		return mapError(err)
	}
	return json.Unmarshal([]byte(raw), dst)
}

func (s *Store) SetSetting(ctx context.Context, key string, value any, actorID string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode setting: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO system_settings(key,value_json,updated_by,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, key, raw, nullableString(actorID), NowMS())
	return mapError(err)
}

func (s *Store) AllSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT key,value_json FROM system_settings ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var value json.RawMessage
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

func (s *Store) Audit(ctx context.Context, id, actorType, actorUserID, action, projectID, targetID, clientIP, requestID string, metadata any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO audit_logs(id,actor_type,actor_user_id,action,project_id,target_id,client_ip,request_id,metadata_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorType, nullableString(actorUserID), action, nullableString(projectID), nullableString(targetID), nullableString(clientIP), nullableString(requestID), raw, NowMS())
	return err
}
