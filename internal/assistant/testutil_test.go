package assistant

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/store"
)

func openTestVault(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
