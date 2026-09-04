package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/altasci/network-storage/backend/internal/security"
)

const shareColumns = `id,project_id,target_node_id,created_by,public_token_hash,code_hash,require_code,code_length,expires_at,disabled_at,created_at`

func scanShare(scanner interface{ Scan(...any) error }) (Share, error) {
	var sh Share
	err := scanner.Scan(&sh.ID, &sh.ProjectID, &sh.TargetNodeID, &sh.CreatedBy, &sh.PublicTokenHash, &sh.CodeHash, &sh.RequireCode, &sh.CodeLength, &sh.ExpiresAt, &sh.DisabledAt, &sh.CreatedAt)
	return sh, mapError(err)
}
func (s *Store) CreateShare(ctx context.Context, sh Share) error {
	if sh.CodeLength == 0 {
		sh.CodeLength = 4
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO shares(`+shareColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, sh.ID, sh.ProjectID, sh.TargetNodeID, sh.CreatedBy, sh.PublicTokenHash, nullableString(sh.CodeHash.String), sh.RequireCode, sh.CodeLength, nullableInt64(sh.ExpiresAt), nil, sh.CreatedAt)
	return mapError(err)
}
func nullableInt64(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
func (s *Store) ShareByID(ctx context.Context, id string) (Share, error) {
	return scanShare(s.DB.QueryRowContext(ctx, `SELECT `+shareColumns+` FROM shares WHERE id=?`, id))
}
func (s *Store) ShareByToken(ctx context.Context, token string) (Share, error) {
	return scanShare(s.DB.QueryRowContext(ctx, `SELECT `+shareColumns+` FROM shares WHERE public_token_hash=?`, security.TokenHash(token)))
}
func (s *Store) ListShares(ctx context.Context, u User) ([]Share, error) {
	query := `SELECT ` + shareColumns + ` FROM shares ORDER BY created_at DESC`
	args := []any{}
	if u.Role != "admin" {
		query = `SELECT ` + shareColumns + ` FROM shares WHERE created_by=? ORDER BY created_at DESC`
		args = append(args, u.ID)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		sh, e := scanShare(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}
func (s *Store) UpdateShare(ctx context.Context, id string, expiresAt sql.NullInt64, disabled bool) error {
	var disabledAt any
	if disabled {
		disabledAt = NowMS()
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE shares SET expires_at=?,disabled_at=? WHERE id=?`, nullableInt64(expiresAt), disabledAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DisableShare(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE shares SET disabled_at=? WHERE id=? AND disabled_at IS NULL`, NowMS(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) IsDescendant(ctx context.Context, rootID, requestedID string) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `WITH RECURSIVE tree(id) AS (SELECT id FROM fs_nodes WHERE id=? AND deleted_at IS NULL UNION ALL SELECT n.id FROM fs_nodes n JOIN tree t ON n.parent_id=t.id WHERE n.deleted_at IS NULL) SELECT EXISTS(SELECT 1 FROM tree WHERE id=?)`, rootID, requestedID).Scan(&ok)
	return ok, err
}

// RecordFailure persists limit state so restarts cannot clear a brute-force ban.
func (s *Store) RecordFailure(ctx context.Context, scopeType, scopeKey string, limit int, window, baseBan time.Duration, escalateAfter int, escalatedBan time.Duration) (time.Duration, error) {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id string
	var failures, banCount int
	var windowStart, historyStart int64
	var bannedUntil sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,failure_count,window_started_at,ban_count,ban_history_started_at,banned_until FROM security_bans WHERE scope_type=? AND scope_key=?`, scopeType, scopeKey).Scan(&id, &failures, &windowStart, &banCount, &historyStart, &bannedUntil)
	if err == sql.ErrNoRows {
		id = security.NewID()
		windowStart = now
		historyStart = now
	} else if err != nil {
		return 0, err
	}
	if bannedUntil.Valid && bannedUntil.Int64 > now {
		return time.Duration(bannedUntil.Int64-now) * time.Millisecond, nil
	}
	if now-windowStart >= window.Milliseconds() {
		failures = 0
		windowStart = now
	}
	if now-historyStart >= 24*time.Hour.Milliseconds() {
		banCount = 0
		historyStart = now
	}
	failures++
	var until any
	var retry time.Duration
	if failures >= limit {
		banCount++
		retry = baseBan
		if escalateAfter > 0 && banCount >= escalateAfter {
			retry = escalatedBan
		}
		until = now + retry.Milliseconds()
		failures = 0
		windowStart = now
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO security_bans(id,scope_type,scope_key,failure_count,window_started_at,ban_count,ban_history_started_at,banned_until,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(scope_type,scope_key) DO UPDATE SET failure_count=excluded.failure_count,window_started_at=excluded.window_started_at,ban_count=excluded.ban_count,ban_history_started_at=excluded.ban_history_started_at,banned_until=excluded.banned_until,updated_at=excluded.updated_at`, id, scopeType, scopeKey, failures, windowStart, banCount, historyStart, until, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return retry, nil
}
func (s *Store) BanRemaining(ctx context.Context, scopeType, scopeKey string) (time.Duration, error) {
	var until sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT banned_until FROM security_bans WHERE scope_type=? AND scope_key=?`, scopeType, scopeKey).Scan(&until)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	remaining := until.Int64 - NowMS()
	if !until.Valid || remaining <= 0 {
		return 0, nil
	}
	return time.Duration(remaining) * time.Millisecond, nil
}
func (s *Store) ClearFailures(ctx context.Context, scopeType, scopeKey string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM security_bans WHERE scope_type=? AND scope_key=?`, scopeType, scopeKey)
	return err
}

func ActiveShare(sh Share, now int64) error {
	if sh.DisabledAt.Valid {
		return fmt.Errorf("share revoked")
	}
	if sh.ExpiresAt.Valid && sh.ExpiresAt.Int64 <= now {
		return fmt.Errorf("share expired")
	}
	return nil
}
