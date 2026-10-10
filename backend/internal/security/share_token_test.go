package security

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestShareTokens(t *testing.T) {
	for i := 0; i < 30; i++ {
		token, err := GenerateShareToken()
		require.NoError(t, err)
		require.Regexp(t, `^[0-9]{30}$`, token)
		require.Regexp(t, `^[0-9]{5}(-[0-9]{5}){5}$`, FormatShareToken(token))
		require.Equal(t, token, NormalizeShareToken(FormatShareToken(token)))
	}
	require.Equal(t, "00000123451234512345", NormalizeShareToken("00000-12345-12345-12345"))
	require.Equal(t, "00000-12345-12345-12345-12345-12345", FormatShareToken("00000"+"12345"+"12345"+"12345"+"12345"+"12345"))
	require.Equal(t, "00000"+"12345"+"12345"+"12345"+"12345"+"12345", NormalizeShareToken("00000-12345-12345-12345-12345-12345"))
	require.Equal(t, "00000-12345-12345-12345", FormatShareToken("00000123451234512345"))
	for _, legacy := range []string{"SPICfKuXzUIZNaNBp1E6NzyMJ4jbc-E2", "12345-abc", "12345-12345"} {
		require.Equal(t, legacy, NormalizeShareToken(legacy))
	}
}
