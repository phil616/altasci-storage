package s3

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/altasci/network-storage/backend/internal/storage/contracttest"
	"github.com/stretchr/testify/require"
)

func TestStorageContract(t *testing.T) {
	configRaw, secretRaw := os.Getenv("ALTASCI_TEST_S3_CONFIG_JSON"), os.Getenv("ALTASCI_TEST_S3_SECRET_JSON")
	if configRaw == "" || secretRaw == "" {
		t.Skip("S3 contract credentials are not configured")
	}
	var config Config
	var secret Secret
	require.NoError(t, json.Unmarshal([]byte(configRaw), &config))
	require.NoError(t, json.Unmarshal([]byte(secretRaw), &secret))
	storage, err := New(context.Background(), config, secret)
	require.NoError(t, err)
	contracttest.Run(t, storage)
}
