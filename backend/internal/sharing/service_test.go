package sharing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodeAndGrant(t *testing.T) {
	code, err := GenerateCode()
	require.NoError(t, err)
	require.Regexp(t, `^\d{4}$`, code)
	require.True(t, ValidCodeFormat(code, CodeLength))
	require.False(t, ValidCodeFormat("12A4", CodeLength))
	require.True(t, ValidCodeFormat("J7MK4XQP", 8))
	hash, err := HashCode(code)
	require.NoError(t, err)
	ok, err := VerifyCode(hash.String, code)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = VerifyCode(hash.String, "99999")
	require.NoError(t, err)
	require.False(t, ok)
	svc := New([]byte("01234567890123456789012345678901"))
	grant, err := svc.Grant("share-id", time.Minute)
	require.NoError(t, err)
	require.NoError(t, svc.ValidateGrant(grant, "share-id"))
	require.Error(t, svc.ValidateGrant(grant, "different"))
}
