package oss

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/altasci/network-storage/backend/internal/storage/contracttest"
	"github.com/stretchr/testify/require"
)

func TestStorageContract(t *testing.T) {
	configRaw, secretRaw := os.Getenv("ALTASCI_TEST_OSS_CONFIG_JSON"), os.Getenv("ALTASCI_TEST_OSS_SECRET_JSON")
	if configRaw == "" || secretRaw == "" {
		t.Skip("OSS contract credentials are not configured")
	}
	var config Config
	var secret Secret
	require.NoError(t, json.Unmarshal([]byte(configRaw), &config))
	require.NoError(t, json.Unmarshal([]byte(secretRaw), &secret))
	storage, err := New(config, secret)
	require.NoError(t, err)
	contracttest.Run(t, storage)
}
