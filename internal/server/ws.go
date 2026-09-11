package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"nhooyr.io/websocket"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
	"github.com/carlallen/RB3EnhancedCompanion/internal/rb3net"
)

// dashboardState is the JSON shape pushed to the browser over /ws: the
// live game state plus, only when it's changed since this connection's
// last message, the song list - which can be large, so it's not worth
// resending on every packet.
type dashboardState struct {
	Connected     bool         `json:"connected"`
	Platform      string       `json:"platform"`
	InGame        bool         `json:"inGame"`
	ScreenName    string       `json:"screenName"`
	SongName      string       `json:"songName"`
	SongArtist    string       `json:"songArtist"`
	SongShortName string       `json:"songShortName"`
	VenueName     string       `json:"venueName"`
	Score         scoreDTO     `json:"score"`
	Band          [4]memberDTO `json:"band"`
	StageKit      stageKitDTO  `json:"stageKit"`
	SongList      []songDTO    `json:"songList,omitempty"`
	// SongListVersion accompanies SongList, so the client can remember which
	// version it has and skip asking for it again on the next connection.
	SongListVersion int `json:"songListVersion,omitempty"`
}

type scoreDTO struct {
	Total   int32    `json:"total"`
	Members [4]int32 `json:"members"`
	Stars   byte     `json:"stars"`
}

type memberDTO struct {
	Exists     bool   `json:"exists"`
	Difficulty string `json:"difficulty"`
	TrackType  string `json:"trackType"`
}

type stageKitDTO struct {
	LED    [8][4]bool `json:"led"`
	Strobe byte       `json:"strobe"`
}

type songDTO struct {
	Shortname string `json:"shortname"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Origin    string `json:"origin"`
	// Source is Origin's human-readable name (see sourceDisplayName) - Origin
	// itself is kept for picking the origin icon file, which is keyed by the
	// raw symbol.
	Source string `json:"source"`

	// Difficulty fields are always sent, even when 0 (no chart for that
	// part) - unlike the nullable fields below, so no omitempty.
	DifficultyBand      int `json:"difficultyBand"`
	DifficultyGuitar    int `json:"difficultyGuitar"`
	DifficultyBass      int `json:"difficultyBass"`
	DifficultyDrum      int `json:"difficultyDrum"`
	DifficultyKeys      int `json:"difficultyKeys"`
	DifficultyVocals    int `json:"difficultyVocals"`
	DifficultyProGuitar int `json:"difficultyProGuitar"`
	DifficultyProBass   int `json:"difficultyProBass"`
	DifficultyProDrum   int `json:"difficultyProDrum"`
	DifficultyProKeys   int `json:"difficultyProKeys"`

	Genre      *string `json:"genre,omitempty"`
	VocalParts *int    `json:"vocalParts,omitempty"`
	Year       *int    `json:"year,omitempty"`
	Length     *int    `json:"lengthMs,omitempty"`

	MetadataDatetime *time.Time `json:"metadataDatetime,omitempty"`

	// HasArt tells the dashboard whether to request this song's custom art
	// (at /static/art/<shortname>_keep.png) or just use its own default
	// image, rather than the server guessing via a fallback chain - see
	// hasArtFile.
	HasArt bool `json:"hasArt"`
}

// hasArtFile reports whether custom album art has been downloaded for
// shortname into artDir (see RB3ECDBCheckWatcher). Checked directly against
// disk on every song list push rather than cached, since that's cheap
// enough and keeps this the single source of truth for HasArt.
func hasArtFile(artDir, shortname string) bool {
	info, err := os.Stat(filepath.Join(artDir, shortname+"_keep.png"))
	return err == nil && !info.IsDir()
}

// newSongDTO converts one rb3net.Song to the JSON shape sent to the
// browser, filling in HasArt from disk (song itself doesn't carry it).
func newSongDTO(song rb3net.Song, hasArt bool) songDTO {
	return songDTO{
		Shortname: song.Shortname,
		Title:     song.Title,
		Artist:    song.Artist,
		Album:     song.Album,
		Origin:    song.Origin,
		Source:    sourceDisplayName(song.Origin),

		DifficultyBand:      song.DifficultyBand,
		DifficultyGuitar:    song.DifficultyGuitar,
		DifficultyBass:      song.DifficultyBass,
		DifficultyDrum:      song.DifficultyDrum,
		DifficultyKeys:      song.DifficultyKeys,
		DifficultyVocals:    song.DifficultyVocals,
		DifficultyProGuitar: song.DifficultyProGuitar,
		DifficultyProBass:   song.DifficultyProBass,
		DifficultyProDrum:   song.DifficultyProDrum,
		DifficultyProKeys:   song.DifficultyProKeys,

		Genre:      song.Genre,
		VocalParts: song.VocalParts,
		Year:       song.Year,
		Length:     song.Length,

		MetadataDatetime: song.MetadataDatetime,
		HasArt:           hasArt,
	}
}

// toDashboardState converts a rb3net.GameState to the JSON shape sent to
// the browser, loading the song list fresh from sqlDB - rather than from
// any in-memory copy - only when includeSongs is set. Returns an error if
// that load fails; d is still otherwise fully populated in that case, just
// without SongList/SongListVersion, so the caller can still send the rest
// of the state.
func toDashboardState(s rb3net.GameState, includeSongs bool, artDir string, sqlDB *sql.DB) (dashboardState, error) {
	d := dashboardState{
		Connected:     s.Connected,
		Platform:      s.Platform,
		InGame:        s.InGame,
		ScreenName:    s.ScreenName,
		SongName:      s.SongName,
		SongArtist:    s.SongArtist,
		SongShortName: s.SongShortName,
		VenueName:     venueDisplayName(s.VenueName),
		Score:         scoreDTO(s.Score),
		StageKit:      stageKitDTO(s.StageKit),
	}
	for i, m := range s.Band {
		d.Band[i] = memberDTO{
			Exists:     m.Exists,
			Difficulty: m.Difficulty.String(),
			TrackType:  m.TrackType.String(),
		}
	}
	if includeSongs {
		songs, err := db.LoadSongs(sqlDB)
		if err != nil {
			return d, err
		}
		d.SongList = make([]songDTO, len(songs))
		for i, song := range songs {
			d.SongList[i] = newSongDTO(rb3net.Song(song), hasArtFile(artDir, song.Shortname))
		}
		d.SongListVersion = s.SongListVersion
	}
	return d, nil
}

// handleWS streams the game state, and the song list whenever the client
// doesn't already have the current version of it, to a browser over a
// websocket: once on connect (unless the client's songVersion query param
// already matches), then again whenever it changes thereafter. Mobile
// browsers reconnect the socket often - screen lock, backgrounding,
// wifi/cellular handoff - and without this a reconnect would otherwise
// mean re-fetching and re-rendering the whole (possibly large) song list,
// images included, even though nothing about it changed.
func handleWS(hub *rb3net.Hub, artDir string, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Printf("ws: accept failed: %v", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "")

		ctx := r.Context()

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		clientSongVersion, _ := strconv.Atoi(r.URL.Query().Get("songVersion"))

		// sentSongVersion tracks the song list version this connection has
		// actually sent the client so far, which only advances on a
		// successful db.LoadSongs - if that fails, it's left as-is so the
		// next state change retries the load rather than the client being
		// silently left on a stale version.
		sentSongVersion := clientSongVersion

		initial := hub.State()
		includeSongs := clientSongVersion != initial.SongListVersion
		d, err := toDashboardState(initial, includeSongs, artDir, sqlDB)
		if err != nil {
			log.Printf("ws: failed to load song list: %v", err)
		} else if includeSongs {
			sentSongVersion = initial.SongListVersion
		}
		if err := writeState(ctx, conn, d); err != nil {
			return
		}

		for {
			select {
			case <-ctx.Done():
				conn.Close(websocket.StatusNormalClosure, "")
				return
			case state, ok := <-ch:
				if !ok {
					return
				}
				includeSongs := state.SongListVersion != sentSongVersion
				d, err := toDashboardState(state, includeSongs, artDir, sqlDB)
				if err != nil {
					log.Printf("ws: failed to load song list: %v", err)
				} else if includeSongs {
					sentSongVersion = state.SongListVersion
				}
				if err := writeState(ctx, conn, d); err != nil {
					return
				}
			}
		}
	}
}

func writeState(ctx context.Context, conn *websocket.Conn, d dashboardState) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data)
}

// handleJump proxies to the console's /jump endpoint, which switches the
// Music Library's selection to the given song.
func handleJump(hub *rb3net.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		shortname := r.URL.Query().Get("shortname")
		if shortname == "" {
			http.Error(w, "missing shortname", http.StatusBadRequest)
			return
		}

		ip := hub.State().SourceIP
		if ip == "" {
			http.Error(w, "console address unknown - waiting for RB3Enhanced", http.StatusServiceUnavailable)
			return
		}

		if err := rb3net.Jump(r.Context(), ip, shortname); err != nil {
			log.Printf("ws: jump proxy failed: %v (console_ip=%s, shortname=%s)", err, ip, shortname)
			http.Error(w, "failed to reach console", http.StatusBadGateway)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
