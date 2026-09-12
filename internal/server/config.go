package server

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
)

// configPageData is the data made available to config.html.
type configPageData struct {
	WLEDDevices []wledDeviceView
	// WLEDError, when set, is shown as a banner explaining why the most
	// recent LED mapping save was rejected. WLEDErrorDeviceID names the
	// device it applies to, so that device's mapping panel starts open.
	WLEDError         string
	WLEDErrorDeviceID int64
}

// wledDeviceView is a WLEDDevice rendered for the config page: its channel
// mapping is laid out as one row per stage position - each holding its
// Red, Green, Blue, and Yellow channel - plus a separate Strobe cell.
type wledDeviceView struct {
	ID      int64
	Name    string
	IP      string
	Enabled bool
	Rows    [][]wledChannelCell
	Strobe  wledChannelCell
	Colours []wledColourCell
}

// wledColourCell is one Colours entry's form field: its key (used to build
// the "colour_KEY" field name), its human-readable label, and its current
// value as a "#rrggbb" string, the format <input type="color"> requires.
type wledColourCell struct {
	Key   string
	Label string
	Value string
}

// wledChannelCell is one channel's form field: its index (used to build the
// "channel_N" field name), its human-readable label, and its current
// comma-separated LED list.
type wledChannelCell struct {
	Index int
	Label string
	Value string
}

// buildWLEDChannelRows groups labels/display - both db.WLEDChannelCount
// long, in the R1,G1,B1,Y1,R2,... order given by db.WLEDChannelLabels -
// into one row of 4 cells per stage position, plus the trailing Strobe
// cell on its own.
func buildWLEDChannelRows(labels, display []string) (rows [][]wledChannelCell, strobe wledChannelCell) {
	rows = make([][]wledChannelCell, 8)
	for pos := 0; pos < 8; pos++ {
		row := make([]wledChannelCell, 4)
		for colour := 0; colour < 4; colour++ {
			idx := pos*4 + colour
			row[colour] = wledChannelCell{Index: idx, Label: labels[idx], Value: display[idx]}
		}
		rows[pos] = row
	}
	strobe = wledChannelCell{Index: db.WLEDStrobeChannel, Label: labels[db.WLEDStrobeChannel], Value: display[db.WLEDStrobeChannel]}
	return rows, strobe
}

func toWLEDDeviceView(d db.WLEDDevice) wledDeviceView {
	display := make([]string, len(d.ChannelLEDs))
	for i, leds := range d.ChannelLEDs {
		strs := make([]string, len(leds))
		for j, led := range leds {
			strs[j] = strconv.Itoa(led)
		}
		display[i] = strings.Join(strs, ",")
	}
	rows, strobe := buildWLEDChannelRows(db.WLEDChannelLabels(), display)
	return wledDeviceView{
		ID:      d.ID,
		Name:    d.Name,
		IP:      d.IP,
		Enabled: d.Enabled,
		Rows:    rows,
		Strobe:  strobe,
		Colours: toWLEDColourCells(d.Colours),
	}
}

// wledColourKeys gives the "colour_KEY" field-name key for each entry of
// db.WLEDColourChannelLabels, in the same order.
func wledColourKeys() []string {
	return []string{"red", "green", "blue", "yellow", "strobe"}
}

// toWLEDColourCells renders a device's packed 24-bit colours as the
// "#rrggbb" strings <input type="color"> requires.
func toWLEDColourCells(colours []uint32) []wledColourCell {
	labels := db.WLEDColourChannelLabels()
	keys := wledColourKeys()
	cells := make([]wledColourCell, len(colours))
	for i, c := range colours {
		r, g, b := db.UnpackWLEDColour(c)
		cells[i] = wledColourCell{Key: keys[i], Label: labels[i], Value: fmt.Sprintf("#%02x%02x%02x", r, g, b)}
	}
	return cells
}

// parseHexColour parses a "#rrggbb" string, the format <input
// type="color"> submits, into its packed 24-bit value.
func parseHexColour(s string) (uint32, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, fmt.Errorf("%q is not a #rrggbb colour", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%q is not a #rrggbb colour", s)
	}
	return uint32(n), nil
}

// parseLEDList parses a comma-separated list of LED indices, e.g. "0, 1,2".
// WARLS's LED index is a single byte (see rb3net.sendWARLS), so every entry
// must be an integer from 0 to 255; anything else is a validation error
// rather than something to quietly drop.
func parseLEDList(s string) ([]int, error) {
	var leds []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return nil, fmt.Errorf("%q is not a number from 0 to 255", part)
		}
		leds = append(leds, n)
	}
	return leds, nil
}

// buildConfigPageData loads the current WLED devices for rendering
// config.html.
func buildConfigPageData(sqlDB *sql.DB) (configPageData, error) {
	devices, err := db.ListWLEDDevices(sqlDB)
	if err != nil {
		return configPageData{}, err
	}
	views := make([]wledDeviceView, len(devices))
	for i, d := range devices {
		views[i] = toWLEDDeviceView(d)
	}
	return configPageData{
		WLEDDevices: views,
	}, nil
}

// overrideChannelDisplay rebuilds the channel rows of the device with the
// given id from submitted - the raw form values just entered, invalid ones
// included - so rejecting a save doesn't lose what the user typed.
func overrideChannelDisplay(views []wledDeviceView, id int64, submitted []string) {
	labels := db.WLEDChannelLabels()
	for i := range views {
		if views[i].ID == id {
			views[i].Rows, views[i].Strobe = buildWLEDChannelRows(labels, submitted)
			return
		}
	}
}

// handleConfig renders the config page, including the currently configured
// WLED devices.
func handleConfig(tmpl *template.Template, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := buildConfigPageData(sqlDB)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// handleAddWLEDDevice adds a new WLED device from a config page form
// submission, then returns to the config page.
func handleAddWLEDDevice(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ip := strings.TrimSpace(r.FormValue("ip"))
		if ip == "" {
			http.Error(w, "ip is required", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("wled-device-name"))
		if _, err := db.AddWLEDDevice(sqlDB, name, ip); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}

// handleDeleteWLEDDevice removes a WLED device named by the "id" form field.
func handleDeleteWLEDDevice(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := db.DeleteWLEDDevice(sqlDB, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}

// handleWLEDDeviceEnabled sets a WLED device's enabled flag from the "id"
// and "enabled" ("0" or "1") form fields.
func handleWLEDDeviceEnabled(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := db.SetWLEDDeviceEnabled(sqlDB, id, r.FormValue("enabled") == "1"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}

// handleWLEDDeviceChannels replaces a WLED device's channel-to-LED mapping
// from the "id" and "channel_0".."channel_32" form fields, in the order
// given by db.WLEDChannelLabels. If any field contains a number outside
// WARLS's addressable 0-255 range, nothing is saved and the config page is
// re-rendered with an error explaining which field(s) to fix, the rest of
// the form left exactly as submitted.
func handleWLEDDeviceChannels(tmpl *template.Template, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		labels := db.WLEDChannelLabels()
		submitted := make([]string, db.WLEDChannelCount)
		channelLEDs := make([][]int, db.WLEDChannelCount)
		var badFields []string
		for i := 0; i < db.WLEDChannelCount; i++ {
			submitted[i] = r.FormValue("channel_" + strconv.Itoa(i))
			leds, err := parseLEDList(submitted[i])
			if err != nil {
				badFields = append(badFields, fmt.Sprintf("%s (%v)", labels[i], err))
				continue
			}
			channelLEDs[i] = leds
		}

		if len(badFields) > 0 {
			data, err := buildConfigPageData(sqlDB)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data.WLEDError = "LED numbers must be from 0 to 255 - nothing was saved. Fix: " + strings.Join(badFields, "; ")
			data.WLEDErrorDeviceID = id
			overrideChannelDisplay(data.WLEDDevices, id, submitted)
			w.WriteHeader(http.StatusUnprocessableEntity)
			if err := tmpl.Execute(w, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		if err := db.SetWLEDDeviceChannelLEDs(sqlDB, id, channelLEDs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}

// handleWLEDDeviceColours replaces a WLED device's Red, Green, Blue,
// Yellow, and Strobe colours from the "id" and "colour_red".."colour_strobe"
// form fields, each a "#rrggbb" string as submitted by <input
// type="color">.
func handleWLEDDeviceColours(sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		keys := wledColourKeys()
		colours := make([]uint32, len(keys))
		for i, key := range keys {
			c, err := parseHexColour(r.FormValue("colour_" + key))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			colours[i] = c
		}

		if err := db.SetWLEDDeviceColours(sqlDB, id, colours); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}
