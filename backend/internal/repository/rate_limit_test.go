package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/altasci/network-storage/backend/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPersistentFailureBan(t *testing.T) {
	store := testutil.Store(t)
	ctx := context.Background()
	for attempt := 1; attempt <= 4; attempt++ {
		retry, err := store.RecordFailure(ctx, "share_ip", "192.0.2.1|share", 5, time.Minute, 15*time.Minute, 3, time.Hour)
		require.NoError(t, err)
		require.Zero(t, retry)
	}
	retry, err := store.RecordFailure(ctx, "share_ip", "192.0.2.1|share", 5, time.Minute, 15*time.Minute, 3, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, retry)
	remaining, err := store.BanRemaining(ctx, "share_ip", "192.0.2.1|share")
	require.NoError(t, err)
	require.Greater(t, remaining, 14*time.Minute)
}
