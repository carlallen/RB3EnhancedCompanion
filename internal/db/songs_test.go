package db

import "testing"

func TestSaveSongsReplacesTable(t *testing.T) {
	sqlDB, err := Open(t.TempDir() + "/test.sql")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	first := []SongRecord{
		{Shortname: "song1", Title: "Song One", Artist: "A", Album: "X", Origin: "rb3"},
		{Shortname: "song2", Title: "Song Two", Artist: "B", Album: "Y", Origin: "rb3"},
	}
	if err := SaveSongs(sqlDB, first); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 2 {
		t.Fatalf("after first save: loaded=%+v err=%v", loaded, err)
	}

	// A later fetch updates song1, drops song2, and adds song3 - the table
	// should end up mirroring exactly this list.
	second := []SongRecord{
		{Shortname: "song1", Title: "Song One Updated", Artist: "A2", Album: "X2", Origin: "rb3"},
		{Shortname: "song3", Title: "Song Three", Artist: "C", Album: "Z", Origin: "rb3"},
	}
	if err := SaveSongs(sqlDB, second); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSongs(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	byShortname := map[string]SongRecord{}
	for _, s := range loaded {
		byShortname[s.Shortname] = s
	}
	if _, ok := byShortname["song2"]; ok {
		t.Fatalf("song2 should have been removed, loaded=%+v", loaded)
	}
	if s, ok := byShortname["song1"]; !ok || s.Title != "Song One Updated" {
		t.Fatalf("song1 should be updated, got %+v", s)
	}
	if _, ok := byShortname["song3"]; !ok {
		t.Fatalf("song3 should be present, loaded=%+v", loaded)
	}

	// An empty fetch (e.g. an emptied Music Library) should clear the table.
	if err := SaveSongs(sqlDB, nil); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadSongs(sqlDB); err != nil || len(loaded) != 0 {
		t.Fatalf("after empty save: loaded=%+v err=%v", loaded, err)
	}
}
