package security

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	require.NoError(t, err)
	require.NotContains(t, hash, "correct horse")
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = VerifyPassword(hash, "incorrect horse battery staple")
	require.NoError(t, err)
	require.False(t, ok)
}
func TestPasswordPolicy(t *testing.T) {
	require.Error(t, ValidatePassword("too short"))
	require.NoError(t, ValidatePassword("长密码 with spaces 可以使用"))
}
func TestSecretBoxBindsAAD(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	ciphertext, err := box.Encrypt([]byte("secret"), "storage-backend:id:v1")
	require.NoError(t, err)
	plain, err := box.Decrypt(ciphertext, "storage-backend:id:v1")
	require.NoError(t, err)
	require.Equal(t, "secret", string(plain))
	_, err = box.Decrypt(ciphertext, "oidc-provider:id:v1")
	require.Error(t, err)
}
