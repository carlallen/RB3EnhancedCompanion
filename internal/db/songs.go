package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SongRecord mirrors rb3net.Song. It's a separate type so this package
// doesn't need to import rb3net. The DifficultyX fields are chart
// difficulty ratings from 0-6; like Genre/VocalParts/Year/Length below,
// they're nullable - nil when no metadata file supplied that part's
// difficulty, rather than a possibly-misleading 0.
type SongRecord struct {
	Shortname string
	Title     string
	Artist    string
	Album     string
	Origin    string

	DifficultyBand      *int
	DifficultyGuitar    *int
	DifficultyBass      *int
	DifficultyDrum      *int
	DifficultyKeys      *int
	DifficultyVocals    *int
	DifficultyProGuitar *int
	DifficultyProBass   *int
	DifficultyProDrum   *int
	DifficultyProKeys   *int

	// Genre, VocalParts, Year and Length are nullable - nil when unknown.
	Genre      *string
	VocalParts *int // 0-3
	Year       *int
	Length     *int // milliseconds

	// MetadataDatetime is the modification time of the metadata JSON file
	// SaveSongs found for this song (nil if none was found). It's
	// recomputed by SaveSongs on every save, so callers don't need to set
	// it themselves.
	MetadataDatetime *time.Time
}

// songsTableColumns are the songs table's columns, shared by
// createSongsTable and rebuildSongsTableNullable (which recreates the
// table under this same definition when migrating an older database).
const songsTableColumns = `
	shortname               TEXT PRIMARY KEY,
	title                   TEXT NOT NULL,
	artist                  TEXT NOT NULL,
	album                   TEXT NOT NULL,
	origin                  TEXT NOT NULL,
	difficulty_band         INTEGER,
	difficulty_guitar       INTEGER,
	difficulty_bass         INTEGER,
	difficulty_drum         INTEGER,
	difficulty_keys         INTEGER,
	difficulty_vocals       INTEGER,
	difficulty_pro_guitar   INTEGER,
	difficulty_pro_bass     INTEGER,
	difficulty_pro_drum     INTEGER,
	difficulty_pro_keys     INTEGER,
	genre                   TEXT,
	vocal_parts             INTEGER,
	year                    INTEGER,
	length                  INTEGER,
	metadata_datetime       DATETIME
`

var createSongsTable = "CREATE TABLE IF NOT EXISTS songs (" + songsTableColumns + ")"

// songNullableIntColumns are the songs columns that must allow NULL -
// checked by rebuildSongsTableNullable, since SQLite can't just ALTER a
// column to drop its NOT NULL constraint.
var songNullableIntColumns = []string{
	"difficulty_band", "difficulty_guitar", "difficulty_bass", "difficulty_drum", "difficulty_keys",
	"difficulty_vocals", "difficulty_pro_guitar", "difficulty_pro_bass", "difficulty_pro_drum", "difficulty_pro_keys",
}

// songMigrationColumns are the songs columns added after the table's
// initial release, in the form "name type-and-default"; migrateSongsTable
// adds any of these missing from an existing database so upgrades don't
// need to drop it.
var songMigrationColumns = []string{
	"difficulty_band INTEGER",
	"difficulty_guitar INTEGER",
	"difficulty_bass INTEGER",
	"difficulty_drum INTEGER",
	"difficulty_keys INTEGER",
	"difficulty_vocals INTEGER",
	"difficulty_pro_guitar INTEGER",
	"difficulty_pro_bass INTEGER",
	"difficulty_pro_drum INTEGER",
	"difficulty_pro_keys INTEGER",
	"genre TEXT",
	"vocal_parts INTEGER",
	"year INTEGER",
	"length INTEGER",
	"metadata_datetime DATETIME",
}

// migrateSongsTable brings an already-existing songs table up to
// songsTableColumns: it adds any songMigrationColumns that are missing
// entirely, then, if needed, rebuilds the table to drop the NOT NULL
// constraint an older version of this schema put on the difficulty
// columns.
func migrateSongsTable(sqlDB *sql.DB) error {
	columns, err := songsColumnInfo(sqlDB)
	if err != nil {
		return err
	}

	for _, colDef := range songMigrationColumns {
		name, _, _ := strings.Cut(colDef, " ")
		if _, ok := columns[name]; ok {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf(`ALTER TABLE songs ADD COLUMN %s`, colDef)); err != nil {
			return err
		}
		columns[name] = false
	}

	needsRebuild := false
	for _, name := range songNullableIntColumns {
		if columns[name] {
			needsRebuild = true
			break
		}
	}
	if needsRebuild {
		return rebuildSongsTableNullable(sqlDB)
	}
	return nil
}

// songsColumnInfo returns the songs table's columns, mapped to whether
// each has a NOT NULL constraint.
func songsColumnInfo(sqlDB *sql.DB) (map[string]bool, error) {
	rows, err := sqlDB.Query(`PRAGMA table_info(songs)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue interface{}
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = notNull != 0
	}
	return columns, rows.Err()
}

// rebuildSongsTableNullable recreates the songs table under the current
// songsTableColumns definition and copies the existing rows across.
// SQLite has no ALTER TABLE form for dropping a column's NOT NULL
// constraint, so this is the only way to lift the NOT NULL DEFAULT 0 an
// older version of this schema put on the difficulty columns - existing
// rows keep whatever 0s they already had until SaveSongs next reprocesses
// them with real metadata.
func rebuildSongsTableNullable(sqlDB *sql.DB) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("CREATE TABLE songs_new (" + songsTableColumns + ")"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO songs_new SELECT
			shortname, title, artist, album, origin,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_drum, difficulty_pro_keys,
			genre, vocal_parts, year, length, metadata_datetime
		FROM songs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE songs`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE songs_new RENAME TO songs`); err != nil {
		return err
	}
	return tx.Commit()
}

// songMetadataFile is the JSON shape of a <shortname>.json metadata file.
// Title/artist/album/shortname aren't read from it - the live console fetch
// is the source of truth for those.
type songMetadataFile struct {
	// All fields are pointers: several metadata files explicitly set some
	// of these to JSON null rather than omitting them, and the diff object
	// omits keys for instruments the song has no chart for entirely - both
	// must come through as "unknown"/"not charted" (nil), never silently
	// become 0 or "".
	Genre *string `json:"genre"`
	Diff  struct {
		Band      *int `json:"band"`
		Guitar    *int `json:"guitar"`
		Bass      *int `json:"bass"`
		Drum      *int `json:"drum"`
		Keys      *int `json:"keys"`
		Vocals    *int `json:"vocals"`
		ProGuitar *int `json:"pro_guitar"`
		ProBass   *int `json:"pro_bass"`
		ProDrum   *int `json:"pro_drum"`
		ProKeys   *int `json:"pro_keys"`
	} `json:"diff"`
	VocalParts   *int `json:"vocal_parts"`
	YearReleased *int `json:"year_released"`
	SongLength   *int `json:"song_length"`
}

// findMetadataFile returns the path and modification time of the first
// <shortname>.json found across metadataDirs, checked in order.
func findMetadataFile(shortname string, metadataDirs []string) (path string, modTime time.Time, found bool) {
	for _, dir := range metadataDirs {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, shortname+".json")
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		return p, info.ModTime(), true
	}
	return "", time.Time{}, false
}

// applySongMetadata sets s's difficulty/genre/vocal-parts/year/length
// fields. If s has a metadata file (found via metadataDirs) newer than the
// metadata it last used (stored.MetadataDatetime), that file is parsed and
// its values applied; otherwise s keeps whatever metadata was already
// stored for it (nothing, if this is a song seen for the first time).
func applySongMetadata(s *SongRecord, metadataDirs []string, stored SongRecord) {
	path, modTime, found := findMetadataFile(s.Shortname, metadataDirs)
	if !found || (stored.MetadataDatetime != nil && !modTime.After(*stored.MetadataDatetime)) {
		copySongMetadata(s, stored)
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("db: failed to read metadata file %s: %v", path, err)
		copySongMetadata(s, stored)
		return
	}
	var meta songMetadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		log.Printf("db: failed to parse metadata file %s: %v", path, err)
		copySongMetadata(s, stored)
		return
	}

	s.DifficultyBand = meta.Diff.Band
	s.DifficultyGuitar = meta.Diff.Guitar
	s.DifficultyBass = meta.Diff.Bass
	s.DifficultyDrum = meta.Diff.Drum
	s.DifficultyKeys = meta.Diff.Keys
	s.DifficultyVocals = meta.Diff.Vocals
	s.DifficultyProGuitar = meta.Diff.ProGuitar
	s.DifficultyProBass = meta.Diff.ProBass
	s.DifficultyProDrum = meta.Diff.ProDrum
	s.DifficultyProKeys = meta.Diff.ProKeys
	s.Genre = meta.Genre
	s.VocalParts = meta.VocalParts
	s.Year = meta.YearReleased
	s.Length = meta.SongLength
	t := modTime
	s.MetadataDatetime = &t
}

// copySongMetadata copies stored's difficulty/genre/vocal-parts/year/
// length/metadata-datetime fields onto s, leaving s's identifying fields
// (shortname, title, artist, album, origin) untouched.
func copySongMetadata(s *SongRecord, stored SongRecord) {
	s.DifficultyBand = stored.DifficultyBand
	s.DifficultyGuitar = stored.DifficultyGuitar
	s.DifficultyBass = stored.DifficultyBass
	s.DifficultyDrum = stored.DifficultyDrum
	s.DifficultyKeys = stored.DifficultyKeys
	s.DifficultyVocals = stored.DifficultyVocals
	s.DifficultyProGuitar = stored.DifficultyProGuitar
	s.DifficultyProBass = stored.DifficultyProBass
	s.DifficultyProDrum = stored.DifficultyProDrum
	s.DifficultyProKeys = stored.DifficultyProKeys
	s.Genre = stored.Genre
	s.VocalParts = stored.VocalParts
	s.Year = stored.Year
	s.Length = stored.Length
	s.MetadataDatetime = stored.MetadataDatetime
}

// nullableSongMetadata holds sql.Scan destinations for every nullable
// songs column (the ten difficulties plus genre/vocal-parts/year/length/
// metadata-datetime), in the column order shared by loadStoredSongMetadata
// and LoadSongs's queries.
type nullableSongMetadata struct {
	difficultyBand      sql.NullInt64
	difficultyGuitar    sql.NullInt64
	difficultyBass      sql.NullInt64
	difficultyDrum      sql.NullInt64
	difficultyKeys      sql.NullInt64
	difficultyVocals    sql.NullInt64
	difficultyProGuitar sql.NullInt64
	difficultyProBass   sql.NullInt64
	difficultyProDrum   sql.NullInt64
	difficultyProKeys   sql.NullInt64
	genre               sql.NullString
	vocalParts          sql.NullInt64
	year                sql.NullInt64
	length              sql.NullInt64
	metadataDatetime    sql.NullTime
}

// dests returns Scan destinations for n's fields, in column order.
func (n *nullableSongMetadata) dests() []interface{} {
	return []interface{}{
		&n.difficultyBand, &n.difficultyGuitar, &n.difficultyBass, &n.difficultyDrum, &n.difficultyKeys,
		&n.difficultyVocals, &n.difficultyProGuitar, &n.difficultyProBass, &n.difficultyProDrum, &n.difficultyProKeys,
		&n.genre, &n.vocalParts, &n.year, &n.length, &n.metadataDatetime,
	}
}

// applyTo sets s's corresponding fields to nil, or a pointer to the scanned
// value, for each of n's columns that came back non-NULL.
func (n nullableSongMetadata) applyTo(s *SongRecord) {
	s.DifficultyBand = nullIntPtr(n.difficultyBand)
	s.DifficultyGuitar = nullIntPtr(n.difficultyGuitar)
	s.DifficultyBass = nullIntPtr(n.difficultyBass)
	s.DifficultyDrum = nullIntPtr(n.difficultyDrum)
	s.DifficultyKeys = nullIntPtr(n.difficultyKeys)
	s.DifficultyVocals = nullIntPtr(n.difficultyVocals)
	s.DifficultyProGuitar = nullIntPtr(n.difficultyProGuitar)
	s.DifficultyProBass = nullIntPtr(n.difficultyProBass)
	s.DifficultyProDrum = nullIntPtr(n.difficultyProDrum)
	s.DifficultyProKeys = nullIntPtr(n.difficultyProKeys)
	if n.genre.Valid {
		g := n.genre.String
		s.Genre = &g
	}
	s.VocalParts = nullIntPtr(n.vocalParts)
	s.Year = nullIntPtr(n.year)
	s.Length = nullIntPtr(n.length)
	if n.metadataDatetime.Valid {
		t := n.metadataDatetime.Time
		s.MetadataDatetime = &t
	}
}

func nullIntPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// loadStoredSongMetadata returns the difficulty/genre/vocal-parts/year/
// length/metadata-datetime fields currently stored for every song, keyed by
// shortname, so SaveSongs can tell whether a song's metadata file has
// changed since it was last applied.
func loadStoredSongMetadata(tx *sql.Tx) (map[string]SongRecord, error) {
	rows, err := tx.Query(`
		SELECT
			shortname,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_drum, difficulty_pro_keys,
			genre, vocal_parts, year, length, metadata_datetime
		FROM songs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]SongRecord)
	for rows.Next() {
		var s SongRecord
		var n nullableSongMetadata
		dest := append([]interface{}{&s.Shortname}, n.dests()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		n.applyTo(&s)
		result[s.Shortname] = s
	}
	return result, rows.Err()
}

// SaveSongs replaces the songs table's contents with songs: rows for
// shortnames no longer present are deleted, the rest are inserted or
// updated, so the table always mirrors the Music Library as of the most
// recent fetch.
//
// For each song, metadataDirs is checked in order for a <shortname>.json
// metadata file. If the file found is newer than the metadata the song last
// used, it's (re)parsed and applied; otherwise the song's previously stored
// metadata is kept as-is. This is done here, rather than by the caller, so
// every insert path gets it consistently. songs is mutated in place with
// the resulting values.
func SaveSongs(sqlDB *sql.DB, songs []SongRecord, metadataDirs ...string) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stored, err := loadStoredSongMetadata(tx)
	if err != nil {
		return err
	}
	for i := range songs {
		applySongMetadata(&songs[i], metadataDirs, stored[songs[i].Shortname])
	}

	stmt, err := tx.Prepare(`
		INSERT INTO songs (
			shortname, title, artist, album, origin,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_drum, difficulty_pro_keys,
			genre, vocal_parts, year, length, metadata_datetime
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(shortname) DO UPDATE SET
			title = excluded.title,
			artist = excluded.artist,
			album = excluded.album,
			origin = excluded.origin,
			difficulty_band = excluded.difficulty_band,
			difficulty_guitar = excluded.difficulty_guitar,
			difficulty_bass = excluded.difficulty_bass,
			difficulty_drum = excluded.difficulty_drum,
			difficulty_keys = excluded.difficulty_keys,
			difficulty_vocals = excluded.difficulty_vocals,
			difficulty_pro_guitar = excluded.difficulty_pro_guitar,
			difficulty_pro_bass = excluded.difficulty_pro_bass,
			difficulty_pro_drum = excluded.difficulty_pro_drum,
			difficulty_pro_keys = excluded.difficulty_pro_keys,
			genre = excluded.genre,
			vocal_parts = excluded.vocal_parts,
			year = excluded.year,
			length = excluded.length,
			metadata_datetime = excluded.metadata_datetime`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	shortnames := make([]interface{}, len(songs))
	placeholders := make([]string, len(songs))
	for i := range songs {
		s := songs[i]
		if _, err := stmt.Exec(
			s.Shortname, s.Title, s.Artist, s.Album, s.Origin,
			s.DifficultyBand, s.DifficultyGuitar, s.DifficultyBass, s.DifficultyDrum, s.DifficultyKeys,
			s.DifficultyVocals, s.DifficultyProGuitar, s.DifficultyProBass, s.DifficultyProDrum, s.DifficultyProKeys,
			s.Genre, s.VocalParts, s.Year, s.Length, s.MetadataDatetime,
		); err != nil {
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
	rows, err := sqlDB.Query(`
		SELECT
			shortname, title, artist, album, origin,
			difficulty_band, difficulty_guitar, difficulty_bass, difficulty_drum, difficulty_keys,
			difficulty_vocals, difficulty_pro_guitar, difficulty_pro_bass, difficulty_pro_drum, difficulty_pro_keys,
			genre, vocal_parts, year, length, metadata_datetime
		FROM songs ORDER BY title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []SongRecord
	for rows.Next() {
		var s SongRecord
		var n nullableSongMetadata
		dest := append([]interface{}{&s.Shortname, &s.Title, &s.Artist, &s.Album, &s.Origin}, n.dests()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		n.applyTo(&s)
		songs = append(songs, s)
	}
	return songs, rows.Err()
}
