package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

type SecretBox struct{ aead cipher.AEAD }

func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, errors.New("master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

func LoadMasterKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if len(b) != 32 {
		return nil, errors.New("master key file must contain exactly 32 raw bytes")
	}
	return b, nil
}

func CreateMasterKey(path string) ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, fmt.Errorf("sync master key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close master key: %w", err)
	}
	return b, nil
}

func (b *SecretBox) Encrypt(plaintext []byte, aad string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate encryption nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, plaintext, []byte(aad))
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Decrypt(encoded, aad string) ([]byte, error) {
	if len(encoded) < 3 || encoded[:3] != "v1:" {
		return nil, errors.New("unsupported ciphertext version")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(encoded[3:])
	if err != nil || len(sealed) < b.aead.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	nonce, ciphertext := sealed[:b.aead.NonceSize()], sealed[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return nil, errors.New("decrypt secret: authentication failed")
	}
	return plain, nil
}
