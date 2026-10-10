package security

import (
	"crypto/rand"
	"math/big"
	"strings"
)

const shareTokenLength = 30

// NormalizeShareToken preserves legacy tokens, including their hyphens.
func NormalizeShareToken(token string) string {
	compact := strings.ReplaceAll(token, "-", "")
	if len(compact) != shareTokenLength && len(compact) != 20 {
		return token
	}
	for _, c := range compact {
		if c < '0' || c > '9' {
			return token
		}
	}
	return compact
}

func GenerateShareToken() (string, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Exp(big.NewInt(10), big.NewInt(shareTokenLength), nil))
	if err != nil {
		return "", err
	}
	value := n.String()
	return strings.Repeat("0", shareTokenLength-len(value)) + value, nil
}

func FormatShareToken(token string) string {
	token = NormalizeShareToken(token)
	if len(token) != shareTokenLength && len(token) != 20 {
		return token
	}
	for _, c := range token {
		if c < '0' || c > '9' {
			return token
		}
	}
	groups := make([]string, 0, len(token)/5)
	for i := 0; i < len(token); i += 5 {
		groups = append(groups, token[i:i+5])
	}
	return strings.Join(groups, "-")
}
