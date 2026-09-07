package server

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
)

// configPageData is the data made available to config.html.
type configPageData struct {
	WLEDDevices       []wledDeviceView
	WLEDChannelLabels []string
}

// wledDeviceView is a WLEDDevice rendered for the config page: its channel
// mapping is flattened to one comma-separated string per channel, ready to
// go straight into a form field's value.
type wledDeviceView struct {
	ID             int64
	Name           string
	IP             string
	Enabled        bool
	ChannelDisplay []string
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
	return wledDeviceView{
		ID:             d.ID,
		Name:           d.Name,
		IP:             d.IP,
		Enabled:        d.Enabled,
		ChannelDisplay: display,
	}
}

// parseLEDList parses a comma-separated list of LED indices, e.g. "0, 1,2",
// silently skipping anything that isn't a non-negative integer.
func parseLEDList(s string) []int {
	var leds []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			continue
		}
		leds = append(leds, n)
	}
	return leds
}

// handleConfig renders the config page, including the currently configured
// WLED devices.
func handleConfig(tmpl *template.Template, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		devices, err := db.ListWLEDDevices(sqlDB)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		views := make([]wledDeviceView, len(devices))
		for i, d := range devices {
			views[i] = toWLEDDeviceView(d)
		}
		data := configPageData{
			WLEDDevices:       views,
			WLEDChannelLabels: db.WLEDChannelLabels(),
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
// given by db.WLEDChannelLabels.
func handleWLEDDeviceChannels(sqlDB *sql.DB) http.HandlerFunc {
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
		channelLEDs := make([][]int, db.WLEDChannelCount)
		for i := range channelLEDs {
			channelLEDs[i] = parseLEDList(r.FormValue("channel_" + strconv.Itoa(i)))
		}
		if err := db.SetWLEDDeviceChannelLEDs(sqlDB, id, channelLEDs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	}
}
