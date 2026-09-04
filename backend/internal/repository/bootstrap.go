package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Initialize atomically creates the first administrator and all bootstrap
// settings. An empty migrated database left by an interrupted older startup
// can be recovered, but a partially or fully initialized database is refused.
func (s *Store) Initialize(ctx context.Context, admin User, settings map[string]any) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var users, configured int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		return fmt.Errorf("inspect users: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_settings").Scan(&configured); err != nil {
		return fmt.Errorf("inspect settings: %w", err)
	}
	if users != 0 || configured != 0 {
		return fmt.Errorf("%w: database is already initialized or contains partial initialization data", ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO users(`+userColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		admin.ID, admin.Email, NormalizeEmail(admin.Email), nullableString(admin.PasswordHash.String), admin.Role, admin.WriteEnabled, admin.Status, admin.CreatedAt, admin.UpdatedAt, nil); err != nil {
		return mapError(err)
	}

	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw, err := json.Marshal(settings[key])
		if err != nil {
			return fmt.Errorf("encode setting %q: %w", key, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings(key,value_json,updated_by,updated_at) VALUES(?,?,?,?)`, key, raw, admin.ID, NowMS()); err != nil {
			return mapError(err)
		}
	}
	return tx.Commit()
}

func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	var admins, webURLs, apiURLs int
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE role='admin' AND status='active'),
		(SELECT COUNT(*) FROM system_settings WHERE key='site.public_web_url'),
		(SELECT COUNT(*) FROM system_settings WHERE key='site.public_api_url')`).Scan(&admins, &webURLs, &apiURLs)
	if err != nil {
		return false, err
	}
	return admins == 1 && webURLs == 1 && apiURLs == 1, nil
}
