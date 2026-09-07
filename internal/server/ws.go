package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"nhooyr.io/websocket"

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
}

// toDashboardState converts a rb3net.GameState to the JSON shape sent to
// the browser, including the song list only when includeSongs is set.
func toDashboardState(s rb3net.GameState, includeSongs bool) dashboardState {
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
		d.SongList = make([]songDTO, len(s.SongList))
		for i, song := range s.SongList {
			d.SongList[i] = songDTO(song)
		}
		d.SongListVersion = s.SongListVersion
	}
	return d
}

// handleWS streams the game state, and the song list whenever the client
// doesn't already have the current version of it, to a browser over a
// websocket: once on connect (unless the client's songVersion query param
// already matches), then again whenever it changes thereafter. Mobile
// browsers reconnect the socket often - screen lock, backgrounding,
// wifi/cellular handoff - and without this a reconnect would otherwise
// mean re-fetching and re-rendering the whole (possibly large) song list,
// images included, even though nothing about it changed.
func handleWS(hub *rb3net.Hub) http.HandlerFunc {
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

		initial := hub.State()
		lastSongVersion := initial.SongListVersion
		includeSongs := clientSongVersion != initial.SongListVersion
		if err := writeState(ctx, conn, toDashboardState(initial, includeSongs)); err != nil {
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
				includeSongs := state.SongListVersion != lastSongVersion
				lastSongVersion = state.SongListVersion
				if err := writeState(ctx, conn, toDashboardState(state, includeSongs)); err != nil {
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
