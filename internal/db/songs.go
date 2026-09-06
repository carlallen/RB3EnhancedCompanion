package db

import (
	"database/sql"
	"strings"
)

// SongRecord mirrors rb3net.Song. It's a separate type so this package
// doesn't need to import rb3net.
type SongRecord struct {
	Shortname string
	Title     string
	Artist    string
	Album     string
	Origin    string
}

const createSongsTable = `
CREATE TABLE IF NOT EXISTS songs (
	shortname TEXT PRIMARY KEY,
	title     TEXT NOT NULL,
	artist    TEXT NOT NULL,
	album     TEXT NOT NULL,
	origin    TEXT NOT NULL
)`

// SaveSongs replaces the songs table's contents with songs: rows for
// shortnames no longer present are deleted, the rest are inserted or
// updated, so the table always mirrors the Music Library as of the most
// recent fetch.
func SaveSongs(sqlDB *sql.DB, songs []SongRecord) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO songs (shortname, title, artist, album, origin)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(shortname) DO UPDATE SET
			title = excluded.title,
			artist = excluded.artist,
			album = excluded.album,
			origin = excluded.origin`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	shortnames := make([]interface{}, len(songs))
	placeholders := make([]string, len(songs))
	for i, s := range songs {
		if _, err := stmt.Exec(s.Shortname, s.Title, s.Artist, s.Album, s.Origin); err != nil {
			return err
		}
		shortnames[i] = s.Shortname
		placeholders[i] = "?"
	}

	deleteQuery := "DELETE FROM songs"
	if len(shortnames) > 0 {
		deleteQuery += " WHERE shortname NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	if _, err := tx.Exec(deleteQuery, shortnames...); err != nil {
		return err
	}

	return tx.Commit()
}

// LoadSongs returns every song stored in the songs table, ordered by title.
func LoadSongs(sqlDB *sql.DB) ([]SongRecord, error) {
	rows, err := sqlDB.Query(`SELECT shortname, title, artist, album, origin FROM songs ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []SongRecord
	for rows.Next() {
		var s SongRecord
		if err := rows.Scan(&s.Shortname, &s.Title, &s.Artist, &s.Album, &s.Origin); err != nil {
			return nil, err
		}
		songs = append(songs, s)
	}
	return songs, rows.Err()
}
