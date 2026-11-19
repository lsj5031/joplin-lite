package store

import (
	"errors"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestWriteTxReservesWALWriterBeforeReading(t *testing.T) {
	db := testDB(t)
	if err := db.SetSetting("test.writer", "initial"); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	peer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	// Probe the writer reservation without waiting on the transaction below.
	if _, err := peer.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatal(err)
	}

	const read = `SELECT value FROM key_values WHERE key = 'test.writer'`
	const write = `UPDATE key_values SET value = ? WHERE key = 'test.writer'`
	err = db.writeTx(func(tx execer) error {
		var value string
		if err := tx.QueryRow(read).Scan(&value); err != nil {
			return err
		}
		_, err := peer.ExecContext(ctx, write, "peer")
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqlite3.SQLITE_BUSY {
			t.Errorf("competing write before the transaction's first write = %v, want SQLITE_BUSY", err)
		}
		if _, err := tx.Exec(write, "transaction"); err != nil {
			return err
		}
		// The reservation must not block WAL readers or expose uncommitted data.
		if err := peer.QueryRowContext(ctx, read).Scan(&value); err != nil || value != "initial" {
			t.Errorf("concurrent read = %q, %v; want initial, nil", value, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write transaction: %v", err)
	}
	if _, err := peer.ExecContext(ctx, write, "peer"); err != nil {
		t.Fatalf("competing write after commit: %v", err)
	}
}
