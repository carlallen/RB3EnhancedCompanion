package server

// venueNames maps RB3Enhanced's raw venue symbol (as reported by the
// VENUE_NAME event) to its display name, sourced from Rock Band 3 Deluxe's
// venue selector localization (_ark/dx/locale/dx_locale_updates.dta) -
// DX replaces the base game's venues with this fixed pool, so these are the
// symbols actually seen on the wire when running it.
var venueNames = map[string]string{
	"arena_01":        "Wilshire Forum",
	"arena_02":        "The Rockacabana",
	"arena_03":        "Gorilla Dome",
	"arena_04":        "Dynamite Palace",
	"arena_06":        "Penfold Dome",
	"arena_07":        "Das Krapfentheater",
	"arena_10":        "Echo Hangar",
	"arena_11":        "The Manor",
	"arena_12":        "Abazan Arena",
	"arena_venues":    "Arena Venues",
	"base_small_club": "The Void",
	"big_club_01":     "Ramp Arts",
	"big_club_02":     "Orion Lounge",
	"big_club_04":     "The Quarter Hole",
	"big_club_05":     "The Snake Pit",
	"big_club_06":     "The Establishment",
	"big_club_07":     "Gas Works Tavern",
	"big_club_08":     "Saville Row",
	"big_club_09":     "Roche De Planète",
	"big_club_10":     "Klub Weisbrot",
	"big_club_11":     "Mjolnir Lodge",
	"big_club_12":     "Discoteca di Luce",
	"big_club_13":     "Teatro Fantasma",
	"big_club_14":     "Gasolina",
	"big_club_15":     "Chronos Club",
	"big_club_17":     "The Bison Room",
	"big_venues":      "Big Club Venues",
	"festival_01":     "Sydney, Australia",
	"festival_02":     "San Diego, CA",
	"festival_venues": "Festival Venues",
	"random":          "[Random]",
	"small_club_01":   "Charles Pub",
	"small_club_02":   "The Wall",
	"small_club_03":   "Heebie Jeebie's",
	"small_club_04":   "El Ocho",
	"small_club_05":   "Jimmy Astros",
	"small_club_06":   "De Magische Tuin",
	"small_club_10":   "Stockholm Syndrome",
	"small_club_11":   "Alice's Free Love Cafe",
	"small_club_13":   "Cheap Shot Records",
	"small_club_14":   "Sweaty's BBQ",
	"small_club_15":   "18 W. 7th St.",
	"small_venues":    "Small Club Venues",
	"venues_video":    "Music Video Venues",
	"video_01":        "Music Video 1",
	"video_02":        "Music Video 2",
	"video_03":        "Music Video 3",
	"video_04":        "Music Video 4",
	"video_05":        "Music Video 5",
	"video_06":        "Music Video 6",
	"video_07":        "Music Video 7",
}

// venueDisplayName returns the human-readable name for a venue symbol, or
// the symbol itself if it's not in venueNames (e.g. a base-game venue DX
// hasn't replaced).
func venueDisplayName(raw string) string {
	if name, ok := venueNames[raw]; ok {
		return name
	}
	return raw
}
