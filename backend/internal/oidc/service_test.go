package oidc

import "testing"

func TestAllowedDomain(t *testing.T) {
	if !allowedDomain(`["example.com"]`, "user@example.com") {
		t.Fatal("allowed verified-email domain rejected")
	}
	if allowedDomain(`["example.com"]`, "user@attacker.example") {
		t.Fatal("untrusted domain accepted")
	}
	if !allowedDomain(`[]`, "anywhere@example.net") {
		t.Fatal("empty allowlist should allow any domain")
	}
}

func TestNonceValidation(t *testing.T) {
	if validNonce("wrong", "expected") {
		t.Fatal("invalid nonce accepted")
	}
	if !validNonce("expected", "expected") {
		t.Fatal("valid nonce rejected")
	}
	if validNonce("", "") {
		t.Fatal("empty nonce accepted")
	}
}
