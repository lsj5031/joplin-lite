package store

import (
	"database/sql"
)

// execer is the write surface both *sql.DB and *sql.Tx expose. Multi-statement
// store operations run through writeTx so they commit atomically and pay for
// one fsync instead of one per statement.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (db *DB) writeTx(fn func(ex execer) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
