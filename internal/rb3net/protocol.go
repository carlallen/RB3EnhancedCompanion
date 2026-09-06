package rb3net

import (
	"encoding/binary"
	"fmt"
)

// Wire format, per RB3Enhanced's include/net_events.h. All multi-byte
// integers are big-endian: the game runs on big-endian hardware (Xbox 360,
// PS3, Wii) and the struct is broadcast as a raw memory copy.

const (
	protocolMagic  = 0x52423345 // 'RB3E'
	headerSize     = 8
	maxPayloadSize = 0xFF
)

// packetType mirrors RB3E_Events_EventTypes. Values are the enum's
// declaration order, which fixes their wire values.
type packetType byte

const (
	ptAlive packetType = iota
	ptState
	ptSongName
	ptSongArtist
	ptSongShortName
	ptScore
	ptStagekit
	ptBandInfo
	ptVenueName
	ptScreenName
	ptDXData
)

// platformID mirrors RB3E_Events_PlatformIDs.
type platformID byte

const (
	platXbox platformID = iota
	platXenia
	platWii
	platDolphin
	platPS3
	platRPCS3
	platUnknown platformID = 0xFF
)

func (p platformID) String() string {
	switch p {
	case platXbox:
		return "Xbox 360"
	case platXenia:
		return "Xenia"
	case platWii:
		return "Wii"
	case platDolphin:
		return "Dolphin"
	case platPS3:
		return "PS3"
	case platRPCS3:
		return "RPCS3"
	default:
		return "Unknown"
	}
}

// packet is one decoded RB3E_EventPacket: a header plus its payload.
type packet struct {
	Version  byte
	Type     packetType
	Platform platformID
	Payload  []byte
}

// parsePacket decodes a single UDP datagram. RB3Enhanced sends exactly one
// event per datagram, so no framing beyond the header/payload split.
func parsePacket(data []byte) (packet, bool) {
	if len(data) < headerSize {
		return packet{}, false
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != protocolMagic {
		return packet{}, false
	}
	version := data[4]
	ptype := packetType(data[5])
	size := int(data[6])
	platform := platformID(data[7])

	if len(data) < headerSize+size {
		return packet{}, false
	}
	return packet{
		Version:  version,
		Type:     ptype,
		Platform: platform,
		Payload:  data[headerSize : headerSize+size],
	}, true
}

// score is RB3E_EventScore: total + per-member scores + star rating.
type score struct {
	Total   int32
	Members [4]int32
	Stars   byte
}

func parseScore(b []byte) (score, bool) {
	if len(b) < 21 {
		return score{}, false
	}
	var s score
	s.Total = int32(binary.BigEndian.Uint32(b[0:4]))
	for i := 0; i < 4; i++ {
		off := 4 + i*4
		s.Members[i] = int32(binary.BigEndian.Uint32(b[off : off+4]))
	}
	s.Stars = b[20]
	return s, true
}

// Difficulty mirrors RB3Enhanced's BandUser.h Difficulty enum.
type Difficulty byte

const (
	DifficultyEasy   Difficulty = 0
	DifficultyMedium Difficulty = 1
	DifficultyHard   Difficulty = 2
	DifficultyExpert Difficulty = 3
)

func (d Difficulty) String() string {
	switch d {
	case DifficultyEasy:
		return "Easy"
	case DifficultyMedium:
		return "Medium"
	case DifficultyHard:
		return "Hard"
	case DifficultyExpert:
		return "Expert"
	default:
		return fmt.Sprintf("Unknown (%d)", byte(d))
	}
}

// TrackType mirrors RB3Enhanced's BandUser.h TrackType enum. Values are not
// sequential in the source - they're declaration order in the enum, which
// doesn't matter for C but is worth noting since it looks odd here.
type TrackType byte

const (
	TrackDrums     TrackType = 0
	TrackGuitar    TrackType = 1
	TrackBass      TrackType = 2
	TrackVocals    TrackType = 3
	TrackKeys      TrackType = 4
	TrackProKeys   TrackType = 5
	TrackProGuitar TrackType = 6
	TrackHarmonies TrackType = 7
	TrackProBass   TrackType = 8
)

func (t TrackType) String() string {
	switch t {
	case TrackDrums:
		return "Drums"
	case TrackGuitar:
		return "Guitar"
	case TrackBass:
		return "Bass"
	case TrackVocals:
		return "Vocals"
	case TrackKeys:
		return "Keys"
	case TrackProKeys:
		return "Pro Keys"
	case TrackProGuitar:
		return "Pro Guitar"
	case TrackHarmonies:
		return "Harmonies"
	case TrackProBass:
		return "Pro Bass"
	default:
		return fmt.Sprintf("Unknown (%d)", byte(t))
	}
}

// bandInfo is RB3E_EventBandInfo: per-slot (up to 4 band members) presence,
// difficulty, and instrument/track type.
type bandInfo struct {
	Exists     [4]bool
	Difficulty [4]Difficulty
	TrackType  [4]TrackType
}

func parseBandInfo(b []byte) (bandInfo, bool) {
	if len(b) < 12 {
		return bandInfo{}, false
	}
	var bi bandInfo
	for i := 0; i < 4; i++ {
		bi.Exists[i] = b[i] != 0
		bi.Difficulty[i] = Difficulty(b[4+i])
		bi.TrackType[i] = TrackType(b[8+i])
	}
	return bi, true
}

// Stage kit "colour" command byte (RightChannel), per the real Harmonix
// Stage Kit protocol: each STAGEKIT packet updates one colour group's 8 LED
// positions (LeftChannel, one bit per position) at a time, or carries a
// fog/strobe/all-off command instead of a colour.
const (
	skColourRed    = 0x80
	skColourGreen  = 0x40
	skColourBlue   = 0x20
	skColourYellow = 0x60
	skStrobeSpeed1 = 0x03
	skStrobeSpeed2 = 0x04
	skStrobeSpeed3 = 0x05
	skStrobeSpeed4 = 0x06
	skStrobeOff    = 0x07
	skAllOff       = 0xFF
	// skFogOn = 0x01, skFogOff = 0x02 are received but not surfaced.
)

// stringPayload trims a trailing NUL, if present, from a string-typed
// packet's payload (ALIVE, SONG_NAME, SONG_ARTIST, ...).
func stringPayload(b []byte) string {
	if n := len(b); n > 0 && b[n-1] == 0 {
		b = b[:n-1]
	}
	return string(b)
}
