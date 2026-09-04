package testutil

import (
	"testing"

	"github.com/altasci/network-storage/backend/internal/database"
	"github.com/altasci/network-storage/backend/internal/repository"
)

func Store(t *testing.T) *repository.Store {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return repository.New(db)
}
