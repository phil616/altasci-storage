package sharing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/altasci/network-storage/backend/internal/repository"
	"github.com/altasci/network-storage/backend/internal/security"
)

const (
	CodeLength         = 4
	codeAlphabet       = "0123456789"
	legacyCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
)

type Service struct{ key []byte }

func New(key []byte) *Service { return &Service{key: key} }
func GenerateCode() (string, error) {
	b := make([]byte, CodeLength)
	for i := range b {
		digit, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = codeAlphabet[digit.Int64()]
	}
	return string(b), nil
}

func ValidCodeFormat(code string, length int) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	alphabet := codeAlphabet
	if length == 8 {
		alphabet = legacyCodeAlphabet
	} else if length != CodeLength {
		return false
	}
	if len(code) != length {
		return false
	}
	for _, character := range code {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
}

type grantClaims struct {
	Typ     string `json:"typ"`
	ShareID string `json:"share_id"`
	Iat     int64  `json:"iat"`
	Exp     int64  `json:"exp"`
}

func (s *Service) Grant(shareID string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(grantClaims{Typ: "share_grant", ShareID: shareID, Iat: now.Unix(), Exp: now.Add(ttl).Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) ValidateGrant(token, shareID string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid grant")
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(got, mac.Sum(nil)) != 1 {
		return errors.New("invalid grant")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid grant")
	}
	var c grantClaims
	if json.Unmarshal(raw, &c) != nil || c.Typ != "share_grant" || c.ShareID != shareID || c.Exp <= time.Now().Unix() {
		return errors.New("invalid grant")
	}
	return nil
}
func HashCode(code string) (sql.NullString, error) {
	hash, err := security.HashSecret(strings.ToUpper(strings.TrimSpace(code)))
	return sql.NullString{String: hash, Valid: err == nil}, err
}
func VerifyCode(hash, code string) (bool, error) {
	return security.VerifyPassword(hash, strings.ToUpper(strings.TrimSpace(code)))
}
func Bearer(header string) string {
	kind, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(kind, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}
func MinTTL(configured time.Duration, expires sql.NullInt64) time.Duration {
	if !expires.Valid {
		return configured
	}
	remaining := time.Until(time.UnixMilli(expires.Int64))
	if remaining < configured {
		return remaining
	}
	return configured
}

var _ = fmt.Sprintf
var _ = repository.ErrNotFound
