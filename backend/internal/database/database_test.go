package database

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// WAL read transactions cannot upgrade stale snapshots to writers. The
// connection configuration must acquire the write reservation at BeginTx so
// concurrent read/modify/write transactions wait instead of failing SQLITE_BUSY.
func TestConcurrentReadModifyWriteTransactions(t *testing.T) {
	db, err := Open(t.TempDir() + "/concurrent.db")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE counter (value INTEGER NOT NULL); INSERT INTO counter VALUES (0)`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const workers = 32
	errors := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errors <- err
				return
			}
			defer tx.Rollback()
			var value int
			if err := tx.QueryRowContext(ctx, `SELECT value FROM counter`).Scan(&value); err != nil {
				errors <- err
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE counter SET value=?`, value+1); err != nil {
				errors <- err
				return
			}
			if err := tx.Commit(); err != nil {
				errors <- err
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var value int
	require.NoError(t, db.QueryRow(`SELECT value FROM counter`).Scan(&value))
	require.Equal(t, workers, value)
}
