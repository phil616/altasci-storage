package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
)

// APIKey stores a delegation ceiling, never a snapshot of the owner's authority.
type APIKey struct {
	ID          string
	UserID      string
	Name        string
	TokenHash   string `json:"-"`
	TokenPrefix string
	Scopes      []string
	AllProjects bool
	ProjectIDs  []string
	CreatedAt   int64
	UpdatedAt   int64
	ExpiresAt   int64
	LastUsedAt  sql.NullInt64
	RevokedAt   sql.NullInt64
}

func (k APIKey) AllowsProject(id string) bool {
	return k.AllProjects || slices.Contains(k.ProjectIDs, id)
}
func (k APIKey) HasScope(scope string) bool { return slices.Contains(k.Scopes, scope) }

const apiKeyColumns = `id,user_id,name,token_hash,token_prefix,scopes_json,all_projects,project_ids_json,created_at,updated_at,expires_at,last_used_at,revoked_at`

func scanAPIKey(row interface{ Scan(...any) error }) (APIKey, error) {
	var k APIKey
	var scopes, projects string
	if err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.TokenHash, &k.TokenPrefix, &scopes, &k.AllProjects, &projects, &k.CreatedAt, &k.UpdatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return k, mapError(err)
	}
	if err := json.Unmarshal([]byte(scopes), &k.Scopes); err != nil {
		return k, err
	}
	return k, json.Unmarshal([]byte(projects), &k.ProjectIDs)
}
func (s *Store) CreateAPIKey(ctx context.Context, k APIKey) error {
	scopes, err := json.Marshal(k.Scopes)
	if err != nil {
		return err
	}
	projects, err := json.Marshal(k.ProjectIDs)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO api_keys(`+apiKeyColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL,NULL)`, k.ID, k.UserID, k.Name, k.TokenHash, k.TokenPrefix, string(scopes), k.AllProjects, string(projects), k.CreatedAt, k.UpdatedAt, k.ExpiresAt)
	return mapError(err)
}
func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE user_id=? ORDER BY created_at DESC,id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, k)
	}
	return result, rows.Err()
}
func (s *Store) APIKeyByID(ctx context.Context, id, userID string) (APIKey, error) {
	return scanAPIKey(s.DB.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE id=? AND user_id=?`, id, userID))
}
func (s *Store) APIKeyByTokenHash(ctx context.Context, hash string, now int64) (APIKey, error) {
	return scanAPIKey(s.DB.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?`, hash, now))
}
func (s *Store) UpdateAPIKey(ctx context.Context, k APIKey) error {
	scopes, err := json.Marshal(k.Scopes)
	if err != nil {
		return err
	}
	projects, err := json.Marshal(k.ProjectIDs)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET name=?,scopes_json=?,all_projects=?,project_ids_json=?,expires_at=?,updated_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, k.Name, string(scopes), k.AllProjects, string(projects), k.ExpiresAt, k.UpdatedAt, k.ID, k.UserID)
	if err != nil {
		return mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) RevokeAPIKey(ctx context.Context, id, userID string) error {
	now := NowMS()
	res, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET revoked_at=COALESCE(revoked_at,?),updated_at=? WHERE id=? AND user_id=?`, now, now, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) TouchAPIKey(ctx context.Context, id string, now int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<?)`, now, id, now-60000)
	return err
}
