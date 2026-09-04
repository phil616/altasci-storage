package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Service struct {
	store  *repository.Store
	box    *security.SecretBox
	apiURL string
}

func New(store *repository.Store, box *security.SecretBox, apiURL string) *Service {
	return &Service{store: store, box: box, apiURL: strings.TrimRight(apiURL, "/")}
}
func (s *Service) Test(ctx context.Context, p repository.OIDCProvider) error {
	_, err := gooidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return fmt.Errorf("OIDC discovery: %w", err)
	}
	return nil
}

func (s *Service) Start(ctx context.Context, providerID, returnTo string, rememberSession bool) (string, error) {
	p, err := s.store.OIDCProviderByID(ctx, providerID)
	if err != nil {
		return "", err
	}
	if !p.Enabled {
		return "", repository.ErrNotFound
	}
	discovery, err := gooidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return "", fmt.Errorf("OIDC discovery: %w", err)
	}
	state, err := security.RandomToken(32)
	if err != nil {
		return "", err
	}
	nonce, err := security.RandomToken(24)
	if err != nil {
		return "", err
	}
	verifier, err := security.RandomToken(32)
	if err != nil {
		return "", err
	}
	hash := security.TokenHash(state)
	ciphertext, err := s.box.Encrypt([]byte(verifier), "oidc-flow:"+hash+":v1")
	if err != nil {
		return "", err
	}
	now := repository.NowMS()
	if err := s.store.CreateOIDCFlow(ctx, repository.OIDCFlow{StateHash: hash, ProviderID: p.ID, PKCEVerifierCiphertext: ciphertext, Nonce: nonce, ReturnTo: returnTo, RememberSession: rememberSession, CreatedAt: now, ExpiresAt: now + 10*time.Minute.Milliseconds()}); err != nil {
		return "", err
	}
	secret, err := s.clientSecret(p)
	if err != nil {
		return "", err
	}
	cfg := oauth2.Config{ClientID: p.ClientID, ClientSecret: secret, Endpoint: discovery.Endpoint(), RedirectURL: s.apiURL + "/api/v1/auth/oidc/" + url.PathEscape(p.ID) + "/callback", Scopes: strings.Fields(p.Scopes)}
	return cfg.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

type Result struct {
	User            repository.User
	ReturnTo        string
	RememberSession bool
}

func (s *Service) Callback(ctx context.Context, providerID, state, code string) (Result, error) {
	if state == "" || code == "" {
		return Result{}, errors.New("missing OIDC state or code")
	}
	f, err := s.store.ConsumeOIDCFlow(ctx, security.TokenHash(state), repository.NowMS())
	if err != nil {
		return Result{}, fmt.Errorf("invalid or expired OIDC state")
	}
	if f.ProviderID != providerID {
		return Result{}, errors.New("OIDC provider mismatch")
	}
	p, err := s.store.OIDCProviderByID(ctx, providerID)
	if err != nil || !p.Enabled {
		return Result{}, repository.ErrNotFound
	}
	verifier, err := s.box.Decrypt(f.PKCEVerifierCiphertext, "oidc-flow:"+f.StateHash+":v1")
	if err != nil {
		return Result{}, err
	}
	discovery, err := gooidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return Result{}, fmt.Errorf("OIDC discovery: %w", err)
	}
	secret, err := s.clientSecret(p)
	if err != nil {
		return Result{}, err
	}
	cfg := oauth2.Config{ClientID: p.ClientID, ClientSecret: secret, Endpoint: discovery.Endpoint(), RedirectURL: s.apiURL + "/api/v1/auth/oidc/" + url.PathEscape(p.ID) + "/callback", Scopes: strings.Fields(p.Scopes)}
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(string(verifier)))
	if err != nil {
		return Result{}, fmt.Errorf("OIDC code exchange: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Result{}, errors.New("OIDC response has no ID token")
	}
	idToken, err := discovery.Verifier(&gooidc.Config{ClientID: p.ClientID}).Verify(ctx, raw)
	if err != nil {
		return Result{}, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	var claims struct {
		Subject       string `json:"sub"`
		Nonce         string `json:"nonce"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Result{}, err
	}
	if !validNonce(claims.Nonce, f.Nonce) {
		return Result{}, errors.New("invalid OIDC nonce")
	}
	if claims.Subject == "" {
		return Result{}, errors.New("OIDC subject is empty")
	}
	if !allowedDomain(p.AllowedEmailDomains, claims.Email) {
		return Result{}, repository.ErrForbidden
	}
	u, err := s.store.ResolveOIDCIdentity(ctx, p, claims.Subject, claims.Email, claims.EmailVerified, security.NewID(), security.NewID())
	if err != nil {
		return Result{}, err
	}
	return Result{User: u, ReturnTo: f.ReturnTo, RememberSession: f.RememberSession}, nil
}
func validNonce(got, want string) bool { return got != "" && got == want }
func (s *Service) clientSecret(p repository.OIDCProvider) (string, error) {
	if !p.ClientSecretCiphertext.Valid {
		return "", nil
	}
	raw, err := s.box.Decrypt(p.ClientSecretCiphertext.String, "oidc-provider:"+p.ID+":v1")
	return string(raw), err
}
func allowedDomain(raw, email string) bool {
	var domains []string
	if json.Unmarshal([]byte(raw), &domains) != nil || len(domains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, allowed := range domains {
		if domain == strings.ToLower(strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}
