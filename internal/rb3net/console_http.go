package rb3net

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
)

// consoleHTTPPort is the TCP port RB3Enhanced's in-game HTTP server listens
// on, on the same console whose UDP broadcast we've been receiving. It's
// the same numeric port as the UDP broadcast (0x524E, 'RN'), just TCP
// instead of UDP.
const consoleHTTPPort = "21070"

// songListScreen is the ScreenName RB3Enhanced reports while the player is
// in the Music Library's song select screen, learned by observation.
const songListScreen = "song_select_screen"

// FetchSongList fetches and parses RB3Enhanced's /list_songs endpoint from
// the console at ip. The console builds this list from whatever's in the
// Music Library at the time of the request, and does the same for every
// caller, so there's no per-request state to worry about here.
func FetchSongList(ctx context.Context, ip string) ([]Song, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:%s/list_songs", ip, consoleHTTPPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("console returned %s", resp.Status)
	}
	return parseSongListINI(resp.Body), nil
}

// parseSongListINI parses RB3Enhanced's /list_songs response: repeated
// "[shortname]\r\nkey=value\r\n...\r\n\r\n" blocks, one per song.
func parseSongListINI(r io.Reader) []Song {
	var songs []Song
	var cur *Song

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			songs = append(songs, Song{})
			cur = &songs[len(songs)-1]
			continue
		}
		if cur == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "shortname":
			cur.Shortname = value
		case "title":
			cur.Title = value
		case "artist":
			cur.Artist = value
		case "album":
			cur.Album = value
		case "origin":
			cur.Origin = value
		}
	}
	return songs
}

// Jump tells the console to select shortname in the Music Library.
func Jump(ctx context.Context, ip, shortname string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	target := fmt.Sprintf("http://%s:%s/jump?shortname=%s", ip, consoleHTTPPort, url.QueryEscape(shortname))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("console returned %s", resp.Status)
	}
	return nil
}

// SongListWatcher fetches the song list from the console's HTTP server the
// first time RB3Enhanced reports the song select screen after each
// successful connection and persists it to DB, bumping the Hub's
// SongListVersion so subscribers, e.g. the web dashboard, know to reload it
// from there.
type SongListWatcher struct {
	Hub *Hub
	DB  *sql.DB
	// MetadataDirs is checked, in order, for a <shortname>.yml metadata
	// file for each song on every save - see db.SaveSongs.
	MetadataDirs []string
}

func (w *SongListWatcher) Run(ctx context.Context) {
	ch := w.Hub.Subscribe()
	defer w.Hub.Unsubscribe(ch)

	wasConnected := false
	fetchedThisConnection := false
	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			if state.Connected && !wasConnected {
				// A new connection: allow one more refresh, the first time
				// the song select screen is visited during it.
				fetchedThisConnection = false
			}
			wasConnected = state.Connected

			if fetchedThisConnection || state.ScreenName != songListScreen || state.SourceIP == "" {
				continue
			}
			fetchedThisConnection = true
			go w.fetch(ctx, state.SourceIP)
		}
	}
}

func (w *SongListWatcher) fetch(ctx context.Context, ip string) {
	songs, err := FetchSongList(ctx, ip)
	if err != nil {
		log.Printf("rb3net: song list fetch failed: %v (console_ip=%s)", err, ip)
		return
	}
	sort.Slice(songs, func(i, j int) bool {
		return strings.ToLower(songs[i].Title) < strings.ToLower(songs[j].Title)
	})

	records := make([]db.SongRecord, len(songs))
	for i, s := range songs {
		records[i] = db.SongRecord(s)
	}
	if err := db.SaveSongs(w.DB, records, w.MetadataDirs...); err != nil {
		log.Printf("rb3net: song list save failed: %v (console_ip=%s)", err, ip)
	}

	w.Hub.Mutate(func(s *GameState) {
		s.SongListVersion++
	})
}
