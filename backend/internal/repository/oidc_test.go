package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestOIDCVerifiedEmailLinking(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	now := repository.NowMS()
	user := repository.User{ID: security.NewID(), Email: "linked@example.com", PasswordHash: sql.NullString{String: "x", Valid: true}, Role: "user", Status: "active", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateUser(ctx, user))
	provider := repository.OIDCProvider{ID: security.NewID(), Name: "idp", Issuer: "https://idp.example", ClientID: "client", Scopes: "openid email", Enabled: true, AutoLinkVerifiedEmail: true, AllowedEmailDomains: `[]`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateOIDCProvider(ctx, provider))
	_, err := store.ResolveOIDCIdentity(ctx, provider, "unverified-sub", user.Email, false, security.NewID(), security.NewID())
	require.True(t, errors.Is(err, repository.ErrForbidden), "unverified email must not be linked: %v", err)
	linked, err := store.ResolveOIDCIdentity(ctx, provider, "verified-sub", user.Email, true, security.NewID(), security.NewID())
	require.NoError(t, err)
	require.Equal(t, user.ID, linked.ID)
}

func TestDeleteOIDCProviderRemovesBindingsAndFlowsButKeepsUsers(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	now := repository.NowMS()
	user := repository.User{ID: security.NewID(), Email: "delete-provider@example.com", PasswordHash: sql.NullString{String: "x", Valid: true}, Role: "user", Status: "active", CreatedAt: now, UpdatedAt: now}
	provider := repository.OIDCProvider{ID: security.NewID(), Name: "deletable-idp", Issuer: "https://idp.example", ClientID: "client", Scopes: "openid email", Enabled: false, AutoLinkVerifiedEmail: true, AllowedEmailDomains: `[]`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateUser(ctx, user))
	require.NoError(t, store.CreateOIDCProvider(ctx, provider))
	_, err := store.ResolveOIDCIdentity(ctx, provider, "subject", user.Email, true, security.NewID(), security.NewID())
	require.NoError(t, err)
	require.NoError(t, store.CreateOIDCFlow(ctx, repository.OIDCFlow{StateHash: "pending", ProviderID: provider.ID, PKCEVerifierCiphertext: "cipher", Nonce: "nonce", ReturnTo: "https://web.example", CreatedAt: now, ExpiresAt: now + 60_000}))

	require.NoError(t, store.DeleteOIDCProvider(ctx, provider.ID))
	_, err = store.OIDCProviderByID(ctx, provider.ID)
	require.ErrorIs(t, err, repository.ErrNotFound)
	_, err = store.UserByID(ctx, user.ID)
	require.NoError(t, err, "deleting a provider must not delete its users")
	var identities, flows int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_identities WHERE provider_id=?`, provider.ID).Scan(&identities))
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oidc_auth_flows WHERE provider_id=?`, provider.ID).Scan(&flows))
	require.Zero(t, identities)
	require.Zero(t, flows)
}

func TestExpiredOIDCStateIsRejectedAndConsumedOnce(t *testing.T) {
	ctx := context.Background()
	store := testutil.Store(t)
	f := repository.OIDCFlow{StateHash: "state", ProviderID: "provider", PKCEVerifierCiphertext: "cipher", Nonce: "nonce", ReturnTo: "https://web.example", RememberSession: true, CreatedAt: 1, ExpiresAt: 2}
	// Provider FK is deliberately satisfied with a minimal provider.
	now := repository.NowMS()
	p := repository.OIDCProvider{ID: "provider", Name: "idp", Issuer: "https://idp.example", ClientID: "client", Scopes: "openid", Enabled: true, AllowedEmailDomains: `[]`, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, store.CreateOIDCProvider(ctx, p))
	require.NoError(t, store.CreateOIDCFlow(ctx, f))
	_, err := store.ConsumeOIDCFlow(ctx, "state", 3)
	require.ErrorIs(t, err, repository.ErrNotFound)
	// A valid flow is single-use.
	f.StateHash, f.ExpiresAt = "fresh", now+10000
	require.NoError(t, store.CreateOIDCFlow(ctx, f))
	consumed, err := store.ConsumeOIDCFlow(ctx, "fresh", now)
	require.NoError(t, err)
	require.True(t, consumed.RememberSession)
	_, err = store.ConsumeOIDCFlow(ctx, "fresh", now)
	require.ErrorIs(t, err, repository.ErrNotFound)
}
