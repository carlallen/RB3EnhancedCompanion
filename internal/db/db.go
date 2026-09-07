// Package db opens the application's SQLite database and holds its schema.
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Open opens (creating if necessary) the SQLite database at path and
// ensures its schema is up to date.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", path))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(createSongsTable); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(createWLEDDevicesTable); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
