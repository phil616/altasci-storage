package repository

import (
	"context"
	"database/sql"
	"fmt"
)

const oidcColumns = `id,name,issuer,client_id,client_secret_ciphertext,scopes,enabled,auto_create_user,auto_link_verified_email,allowed_email_domains,created_at,updated_at`

func scanOIDC(scanner interface{ Scan(...any) error }) (OIDCProvider, error) {
	var p OIDCProvider
	err := scanner.Scan(&p.ID, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecretCiphertext, &p.Scopes, &p.Enabled, &p.AutoCreateUser, &p.AutoLinkVerifiedEmail, &p.AllowedEmailDomains, &p.CreatedAt, &p.UpdatedAt)
	return p, mapError(err)
}
func (s *Store) OIDCProviderByID(ctx context.Context, id string) (OIDCProvider, error) {
	return scanOIDC(s.DB.QueryRowContext(ctx, `SELECT `+oidcColumns+` FROM oidc_providers WHERE id=?`, id))
}
func (s *Store) ListOIDCProviders(ctx context.Context, enabledOnly bool) ([]OIDCProvider, error) {
	q := `SELECT ` + oidcColumns + ` FROM oidc_providers`
	if enabledOnly {
		q += ` WHERE enabled=1`
	}
	q += ` ORDER BY created_at`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OIDCProvider
	for rows.Next() {
		p, e := scanOIDC(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) CreateOIDCProvider(ctx context.Context, p OIDCProvider) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_providers(`+oidcColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Issuer, p.ClientID, nullableString(p.ClientSecretCiphertext.String), p.Scopes, p.Enabled, p.AutoCreateUser, p.AutoLinkVerifiedEmail, p.AllowedEmailDomains, p.CreatedAt, p.UpdatedAt)
	return mapError(err)
}
func (s *Store) UpdateOIDCProvider(ctx context.Context, p OIDCProvider, secret *string) error {
	query := `UPDATE oidc_providers SET name=?,issuer=?,client_id=?,scopes=?,enabled=?,auto_create_user=?,auto_link_verified_email=?,allowed_email_domains=?,updated_at=?`
	args := []any{p.Name, p.Issuer, p.ClientID, p.Scopes, p.Enabled, p.AutoCreateUser, p.AutoLinkVerifiedEmail, p.AllowedEmailDomains, NowMS()}
	if secret != nil {
		query += `,client_secret_ciphertext=?`
		args = append(args, *secret)
	}
	query += ` WHERE id=?`
	args = append(args, p.ID)
	res, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteOIDCProvider(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM oidc_auth_flows WHERE provider_id=?`, id); err != nil {
		return mapError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM external_identities WHERE provider_id=?`, id); err != nil {
		return mapError(err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM oidc_providers WHERE id=?`, id)
	if err != nil {
		return mapError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

type OIDCFlow struct {
	StateHash, ProviderID, PKCEVerifierCiphertext, Nonce, ReturnTo string
	RememberSession                                                bool
	CreatedAt, ExpiresAt                                           int64
}

func (s *Store) CreateOIDCFlow(ctx context.Context, f OIDCFlow) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO oidc_auth_flows(state_hash,provider_id,pkce_verifier_ciphertext,nonce,return_to,remember_session,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`, f.StateHash, f.ProviderID, f.PKCEVerifierCiphertext, f.Nonce, f.ReturnTo, f.RememberSession, f.CreatedAt, f.ExpiresAt)
	return err
}
func (s *Store) ConsumeOIDCFlow(ctx context.Context, stateHash string, now int64) (OIDCFlow, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return OIDCFlow{}, err
	}
	defer tx.Rollback()
	var f OIDCFlow
	err = tx.QueryRowContext(ctx, `SELECT state_hash,provider_id,pkce_verifier_ciphertext,nonce,return_to,remember_session,created_at,expires_at FROM oidc_auth_flows WHERE state_hash=?`, stateHash).Scan(&f.StateHash, &f.ProviderID, &f.PKCEVerifierCiphertext, &f.Nonce, &f.ReturnTo, &f.RememberSession, &f.CreatedAt, &f.ExpiresAt)
	if err != nil {
		return OIDCFlow{}, mapError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oidc_auth_flows WHERE state_hash=?`, stateHash); err != nil {
		return OIDCFlow{}, err
	}
	if err := tx.Commit(); err != nil {
		return OIDCFlow{}, err
	}
	if f.ExpiresAt <= now {
		return OIDCFlow{}, ErrNotFound
	}
	return f, nil
}

func (s *Store) PurgeExpiredOIDCFlows(ctx context.Context, now int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM oidc_auth_flows WHERE expires_at<=?`, now)
	return err
}

func (s *Store) ResolveOIDCIdentity(ctx context.Context, provider OIDCProvider, subject, email string, emailVerified bool, newUserID, newIdentityID string) (User, error) {
	now := NowMS()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var userID string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM external_identities WHERE provider_id=? AND subject=?`, provider.ID, subject).Scan(&userID)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE external_identities SET last_login_at=? WHERE provider_id=? AND subject=?`, now, provider.ID, subject); err != nil {
			return User{}, err
		}
		u, e := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=? AND status='active'`, userID))
		if e != nil {
			return User{}, e
		}
		if err := tx.Commit(); err != nil {
			return User{}, err
		}
		return u, nil
	}
	if err != sql.ErrNoRows {
		return User{}, err
	}
	if provider.AutoLinkVerifiedEmail && emailVerified && email != "" {
		e := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email_normalized=? AND status='active'`, NormalizeEmail(email)).Scan(&userID)
		if e != nil && e != sql.ErrNoRows {
			return User{}, e
		}
	}
	if userID == "" {
		if !provider.AutoCreateUser || email == "" || !emailVerified {
			return User{}, fmt.Errorf("%w: OIDC identity is not linked", ErrForbidden)
		}
		userID = newUserID
		_, err = tx.ExecContext(ctx, `INSERT INTO users(id,email,email_normalized,password_hash,role,write_enabled,status,created_at,updated_at) VALUES(?,?,?,NULL,'user',0,'active',?,?)`, userID, email, NormalizeEmail(email), now, now)
		if err != nil {
			return User{}, mapError(err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO external_identities(id,provider_id,subject,user_id,email_at_link,created_at,last_login_at) VALUES(?,?,?,?,?,?,?)`, newIdentityID, provider.ID, subject, userID, nullableString(email), now, now)
	if err != nil {
		return User{}, mapError(err)
	}
	u, e := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=? AND status='active'`, userID))
	if e != nil {
		return User{}, e
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}
