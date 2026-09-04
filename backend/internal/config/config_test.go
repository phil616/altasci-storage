package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateForInitCreatesAndThenLoadsBootstrapConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")

	cfg, created, err := LoadOrCreateForInit(path)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "127.0.0.1:8080", cfg.Server.Listen)
	require.Equal(t, "./data/altasci-network-storage.db", cfg.Database.Path)
	require.Equal(t, "./data/master.key", cfg.Security.MasterKeyFile)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o007)

	loaded, created, err := LoadOrCreateForInit(path)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, cfg, loaded)
}

func TestLoadOrCreateForInitDoesNotReplaceInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("not = [valid"), 0o640))

	_, created, err := LoadOrCreateForInit(path)
	require.Error(t, err)
	require.False(t, created)
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, "not = [valid", string(raw))
}

func TestValidatePublicURLRequiresExactHTTPSOrigin(t *testing.T) {
	require.NoError(t, ValidatePublicURL("https://web.example.com"))
	require.NoError(t, ValidatePublicURL("https://web.example.com:8443"))

	for _, value := range []string{
		"http://web.example.com",
		"https://web.example.com/",
		"https://web.example.com/path",
		"https://web.example.com?debug=true",
		"https://web.example.com#fragment",
		"https://user:pass@web.example.com",
	} {
		require.Error(t, ValidatePublicURL(value), value)
	}
}
