package repository

import (
	"context"
	"database/sql"
	"fmt"
)

const userColumns = `id,email,email_normalized,password_hash,role,write_enabled,status,created_at,updated_at,last_login_at`
const qualifiedUserColumns = `u.id,u.email,u.email_normalized,u.password_hash,u.role,u.write_enabled,u.status,u.created_at,u.updated_at,u.last_login_at`

func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var u User
	err := scanner.Scan(&u.ID, &u.Email, &u.EmailNormalized, &u.PasswordHash, &u.Role, &u.WriteEnabled, &u.Status, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	return u, mapError(err)
}

func (s *Store) CreateUser(ctx context.Context, user User) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO users(`+userColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		user.ID, user.Email, NormalizeEmail(user.Email), nullableString(user.PasswordHash.String), user.Role, user.WriteEnabled, user.Status, user.CreatedAt, user.UpdatedAt, nil)
	return mapError(err)
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=?`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email_normalized=?`, NormalizeEmail(email)))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id string, email *string, writeEnabled *bool, status *string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	u, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		return User{}, err
	}
	if email != nil {
		u.Email, u.EmailNormalized = *email, NormalizeEmail(*email)
	}
	if writeEnabled != nil {
		u.WriteEnabled = *writeEnabled
	}
	if status != nil {
		u.Status = *status
	}
	u.UpdatedAt = NowMS()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email=?,email_normalized=?,write_enabled=?,status=?,updated_at=? WHERE id=?`, u.Email, u.EmailNormalized, u.WriteEnabled, u.Status, u.UpdatedAt, id); err != nil {
		return User{}, mapError(err)
	}
	if status != nil && *status == "disabled" {
		if u.Role == "admin" {
			return User{}, fmt.Errorf("%w: active administrator cannot be disabled", ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, u.UpdatedAt, id); err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) SetPassword(ctx context.Context, id, hash string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, hash, now, id)
	if err != nil {
		return mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkLogin(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET last_login_at=? WHERE id=?`, NowMS(), id)
	return err
}

func (s *Store) TransferAdmin(ctx context.Context, oldID, newID string) error {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRole, newRole, newStatus string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=?`, oldID).Scan(&oldRole); err != nil {
		return mapError(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT role,status FROM users WHERE id=?`, newID).Scan(&newRole, &newStatus); err != nil {
		return mapError(err)
	}
	if oldRole != "admin" || newRole != "user" || newStatus != "active" {
		return fmt.Errorf("%w: invalid administrator transfer", ErrConflict)
	}
	// The unique partial index requires demotion before promotion inside the same transaction.
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role='user',updated_at=? WHERE id=?`, now, oldID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role='admin',write_enabled=1,updated_at=? WHERE id=?`, now, newID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id IN (?,?) AND revoked_at IS NULL`, now, oldID, newID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,idle_expires_at,absolute_expires_at,ip,user_agent) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, session.CSRFTokenHash, session.CreatedAt, session.LastSeenAt, session.IdleExpiresAt, session.AbsoluteExpiresAt, session.IP, session.UserAgent)
	return mapError(err)
}

func (s *Store) SessionByTokenHash(ctx context.Context, hash string, now int64) (Session, User, error) {
	var ss Session
	row := s.DB.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.token_hash,s.csrf_token_hash,s.created_at,s.last_seen_at,s.idle_expires_at,s.absolute_expires_at,s.ip,s.user_agent,s.revoked_at,`+qualifiedUserColumns+`
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.idle_expires_at>? AND s.absolute_expires_at>? AND u.status='active'`, hash, now, now)
	var u User
	err := row.Scan(&ss.ID, &ss.UserID, &ss.TokenHash, &ss.CSRFTokenHash, &ss.CreatedAt, &ss.LastSeenAt, &ss.IdleExpiresAt, &ss.AbsoluteExpiresAt, &ss.IP, &ss.UserAgent, &ss.RevokedAt,
		&u.ID, &u.Email, &u.EmailNormalized, &u.PasswordHash, &u.Role, &u.WriteEnabled, &u.Status, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	return ss, u, mapError(err)
}

func (s *Store) TouchSession(ctx context.Context, id string, idleExpiresAt int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?,idle_expires_at=? WHERE id=?`, NowMS(), idleExpiresAt, id)
	return err
}

func (s *Store) RotateCSRF(ctx context.Context, id, csrfHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET csrf_token_hash=? WHERE id=? AND revoked_at IS NULL`, csrfHash, id)
	return err
}

func (s *Store) RevokeSession(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, NowMS(), id)
	return err
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, NowMS(), userID)
	return err
}

var _ = sql.ErrNoRows
