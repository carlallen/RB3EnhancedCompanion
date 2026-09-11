// Package rb3net listens for RB3Enhanced's UDP game-state broadcast and
// distributes decoded updates to subscribers, and talks to RB3Enhanced's
// in-game HTTP server for the song list and jump commands.
package rb3net

import (
	"sync"
	"time"
)

// BandMember is one of the up to 4 instrument/vocal slots reported by a
// BAND_INFO packet.
type BandMember struct {
	Exists     bool
	Difficulty Difficulty
	TrackType  TrackType
}

// Score is the latest SCORE packet's contents.
type Score struct {
	Total   int32
	Members [4]int32
	Stars   byte
}

// StageKit is the 8 physical LED positions x 4 colours (Red, Green, Blue,
// Yellow) of a Rock Band Stage Kit, assembled from a stream of STAGEKIT
// packets that each update one colour's 8-bit mask at a time.
type StageKit struct {
	LED [8][4]bool
	// Strobe is 0 when off, or 1-4 for the active strobe speed.
	Strobe byte
}

func (sk *StageKit) applyColour(colourIdx int, ledMask byte) {
	for pos := 0; pos < 8; pos++ {
		sk.LED[pos][colourIdx] = ledMask&(1<<uint(pos)) != 0
	}
}

// Song is one entry from RB3Enhanced's console-side /list_songs response.
// The DifficultyX fields are chart difficulty ratings from 0-7, where 0
// means the song has no chart for that part - including every field here
// until db.SaveSongs fills them in from a metadata file, since the
// console's own song list doesn't report them.
type Song struct {
	Shortname string
	Title     string
	Artist    string
	Album     string
	Origin    string

	DifficultyBand      int
	DifficultyGuitar    int
	DifficultyBass      int
	DifficultyDrum      int
	DifficultyKeys      int
	DifficultyVocals    int
	DifficultyProGuitar int
	DifficultyProBass   int
	DifficultyProDrum   int
	DifficultyProKeys   int

	// Genre, VocalParts, Year and Length are nullable - nil when unknown.
	Genre      *string
	VocalParts *int // 0-3
	Year       *int
	Length     *int // milliseconds

	// MetadataDatetime is the modification time of the metadata JSON file
	// db.SaveSongs found for this song (nil if none was found).
	MetadataDatetime *time.Time
}

// GameState is the latest known state of the game, assembled from whichever
// packets RB3Enhanced has sent so far.
type GameState struct {
	Connected     bool
	Platform      string
	SourceIP      string
	InGame        bool
	SongName      string
	SongArtist    string
	SongShortName string
	VenueName     string
	ScreenName    string
	Score         Score
	Band          [4]BandMember
	StageKit      StageKit

	// The song list itself is fetched from the console's own HTTP server the
	// first time it reports the song select screen after each connection -
	// see SongListWatcher - and persisted straight to the database; it's
	// never cached here, and is instead loaded fresh from the database any
	// time it needs to be sent to the web frontend (see
	// server.toDashboardState). SongListVersion increments each time the
	// stored song list changes, so subscribers can tell without re-querying
	// the database on every state update.
	SongListVersion int
}

// Hub keeps the current GameState and fans out updates to subscribers.
type Hub struct {
	mu    sync.RWMutex
	state GameState

	subMu sync.Mutex
	subs  map[chan GameState]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan GameState]struct{})}
}

func (h *Hub) State() GameState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

// Mutate applies fn to the current state under lock and notifies
// subscribers with the result.
func (h *Hub) Mutate(fn func(*GameState)) {
	h.mu.Lock()
	fn(&h.state)
	s := h.state
	h.mu.Unlock()

	h.subMu.Lock()
	defer h.subMu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- s:
		default:
			// drop if the subscriber hasn't consumed the last update yet;
			// it will catch up on the next one
		}
	}
}

// Subscribe returns a channel that receives every state update until
// Unsubscribe is called with the same channel.
func (h *Hub) Subscribe() chan GameState {
	ch := make(chan GameState, 1)
	h.subMu.Lock()
	h.subs[ch] = struct{}{}
	h.subMu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan GameState) {
	h.subMu.Lock()
	delete(h.subs, ch)
	h.subMu.Unlock()
	close(ch)
}
