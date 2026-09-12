package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// WLEDDevice is a WLED-powered LED strip mapped to mirror the stage kit's
// lights; see rb3net.WLEDWatcher for what actually sends it data.
type WLEDDevice struct {
	ID      int64
	Name    string
	IP      string
	Enabled bool

	// ChannelLEDs maps each of the WLEDChannelCount stage kit channels
	// (see WLEDChannelLabels) to the physical WLED LED indices that should
	// mirror it. A channel can map to zero, one, or several LEDs. Always
	// has length WLEDChannelCount; see DefaultWLEDChannelLEDs.
	ChannelLEDs [][]int

	// Colours gives the RGB colour shown on a device's LEDs for each of the
	// WLEDColourChannelCount stage kit colours (see WLEDColourChannelLabels),
	// packed as a 24-bit 0xRRGGBB integer. Always has length
	// WLEDColourChannelCount; see DefaultWLEDColours and PackWLEDColour.
	Colours []uint32
}

// WLEDColourChannelCount is Red, Green, Blue, and Yellow - the stage kit's
// four lit colours - plus Strobe.
const WLEDColourChannelCount = 5

// WLEDStrobeColourIndex is the Strobe colour's index within a WLEDDevice's
// Colours.
const WLEDStrobeColourIndex = WLEDColourChannelCount - 1

// WLEDColourChannelLabels names each entry of WLEDDevice.Colours, in order.
func WLEDColourChannelLabels() []string {
	return []string{"Red", "Green", "Blue", "Yellow", "Strobe"}
}

// Default colours for a newly added device, matching the stage kit
// hardware's own LED colours; Strobe defaults to white.
const (
	defaultWLEDColourRed    = 0xFF0000
	defaultWLEDColourGreen  = 0x00FF00
	defaultWLEDColourBlue   = 0x0000FF
	defaultWLEDColourYellow = 0xFFFF00
	defaultWLEDColourStrobe = 0xFFFFFF
)

// DefaultWLEDColours returns the stage kit's traditional colours, in
// WLEDColourChannelLabels order, used for a newly added device.
func DefaultWLEDColours() []uint32 {
	return []uint32{
		defaultWLEDColourRed,
		defaultWLEDColourGreen,
		defaultWLEDColourBlue,
		defaultWLEDColourYellow,
		defaultWLEDColourStrobe,
	}
}

// PackWLEDColour packs 0-255 r, g, b values into the single 24-bit integer
// (0xRRGGBB) a colour column stores.
func PackWLEDColour(r, g, b byte) uint32 {
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// UnpackWLEDColour splits a packed 24-bit colour back into its r, g, b
// bytes.
func UnpackWLEDColour(c uint32) (r, g, b byte) {
	return byte(c >> 16), byte(c >> 8), byte(c)
}

// WLEDChannelCount is the 8 stage kit positions x 4 colours (Red, Green,
// Blue, Yellow), plus one final channel for Strobe.
const WLEDChannelCount = 8*4 + 1

// WLEDStrobeChannel is the Strobe channel's index within a WLEDDevice's
// ChannelLEDs.
const WLEDStrobeChannel = WLEDChannelCount - 1

// WLEDChannelLabels returns a human-readable label for each of the
// WLEDChannelCount channels - "R1", "G1", "B1", "Y1", "R2", ... "Y8",
// "Strobe" - in the same order as WLEDDevice.ChannelLEDs.
func WLEDChannelLabels() []string {
	labels := make([]string, WLEDChannelCount)
	colours := [4]byte{'R', 'G', 'B', 'Y'}
	for c := 0; c < WLEDStrobeChannel; c++ {
		pos, colour := c/4, c%4
		labels[c] = fmt.Sprintf("%c%d", colours[colour], pos+1)
	}
	labels[WLEDStrobeChannel] = "Strobe"
	return labels
}

// DefaultWLEDChannelLEDs returns the identity mapping - channel i to
// physical LED i - used for a newly added device.
func DefaultWLEDChannelLEDs() [][]int {
	m := make([][]int, WLEDChannelCount)
	for i := range m {
		m[i] = []int{i}
	}
	return m
}

var createWLEDDevicesTable = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS wled_devices (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	name           TEXT NOT NULL DEFAULT '',
	ip             TEXT NOT NULL,
	channel_leds   TEXT NOT NULL DEFAULT '',
	enabled        INTEGER NOT NULL DEFAULT 1,
	colour_red     INTEGER NOT NULL DEFAULT %d,
	colour_green   INTEGER NOT NULL DEFAULT %d,
	colour_blue    INTEGER NOT NULL DEFAULT %d,
	colour_yellow  INTEGER NOT NULL DEFAULT %d,
	colour_strobe  INTEGER NOT NULL DEFAULT %d
)`,
	defaultWLEDColourRed, defaultWLEDColourGreen, defaultWLEDColourBlue,
	defaultWLEDColourYellow, defaultWLEDColourStrobe)

// wledColourColumns lists the wled_devices colour columns, in
// WLEDColourChannelLabels order, alongside their ALTER TABLE definitions -
// used to bring an already-existing table (created before these columns
// existed) up to date.
var wledColourColumns = []string{
	fmt.Sprintf("colour_red INTEGER NOT NULL DEFAULT %d", defaultWLEDColourRed),
	fmt.Sprintf("colour_green INTEGER NOT NULL DEFAULT %d", defaultWLEDColourGreen),
	fmt.Sprintf("colour_blue INTEGER NOT NULL DEFAULT %d", defaultWLEDColourBlue),
	fmt.Sprintf("colour_yellow INTEGER NOT NULL DEFAULT %d", defaultWLEDColourYellow),
	fmt.Sprintf("colour_strobe INTEGER NOT NULL DEFAULT %d", defaultWLEDColourStrobe),
}

// migrateWLEDDevicesTable adds any wledColourColumns missing from an
// already-existing wled_devices table (created before colours existed).
func migrateWLEDDevicesTable(sqlDB *sql.DB) error {
	rows, err := sqlDB.Query(`PRAGMA table_info(wled_devices)`)
	if err != nil {
		return err
	}
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
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, colDef := range wledColourColumns {
		name, _, _ := strings.Cut(colDef, " ")
		if columns[name] {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf(`ALTER TABLE wled_devices ADD COLUMN %s`, colDef)); err != nil {
			return err
		}
	}
	return nil
}

func encodeChannelLEDs(channelLEDs [][]int) (string, error) {
	b, err := json.Marshal(channelLEDs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeChannelLEDs parses a channel_leds column value, falling back to the
// identity mapping if it's empty or malformed.
func decodeChannelLEDs(s string) [][]int {
	var channelLEDs [][]int
	if err := json.Unmarshal([]byte(s), &channelLEDs); err != nil || len(channelLEDs) != WLEDChannelCount {
		return DefaultWLEDChannelLEDs()
	}
	return channelLEDs
}

// ListWLEDDevices returns every configured WLED device, in the order they
// were added.
func ListWLEDDevices(sqlDB *sql.DB) ([]WLEDDevice, error) {
	rows, err := sqlDB.Query(`
		SELECT id, name, ip, channel_leds, enabled,
			colour_red, colour_green, colour_blue, colour_yellow, colour_strobe
		FROM wled_devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []WLEDDevice
	for rows.Next() {
		var d WLEDDevice
		var channelLEDs string
		d.Colours = make([]uint32, WLEDColourChannelCount)
		if err := rows.Scan(&d.ID, &d.Name, &d.IP, &channelLEDs, &d.Enabled,
			&d.Colours[0], &d.Colours[1], &d.Colours[2], &d.Colours[3], &d.Colours[4]); err != nil {
			return nil, err
		}
		d.ChannelLEDs = decodeChannelLEDs(channelLEDs)
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// AddWLEDDevice inserts a new WLED device, enabled and with the default
// (identity) channel mapping, and returns its assigned ID.
func AddWLEDDevice(sqlDB *sql.DB, name, ip string) (int64, error) {
	channelLEDs, err := encodeChannelLEDs(DefaultWLEDChannelLEDs())
	if err != nil {
		return 0, err
	}
	res, err := sqlDB.Exec(`
		INSERT INTO wled_devices (name, ip, channel_leds, enabled) VALUES (?, ?, ?, 1)`,
		name, ip, channelLEDs)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteWLEDDevice removes the WLED device with the given id.
func DeleteWLEDDevice(sqlDB *sql.DB, id int64) error {
	_, err := sqlDB.Exec(`DELETE FROM wled_devices WHERE id = ?`, id)
	return err
}

// SetWLEDDeviceEnabled sets whether the WLED device with the given id is
// enabled, without touching its other configuration.
func SetWLEDDeviceEnabled(sqlDB *sql.DB, id int64, enabled bool) error {
	_, err := sqlDB.Exec(`UPDATE wled_devices SET enabled = ? WHERE id = ?`, enabled, id)
	return err
}

// SetWLEDDeviceChannelLEDs replaces the channel-to-LED mapping for the WLED
// device with the given id.
func SetWLEDDeviceChannelLEDs(sqlDB *sql.DB, id int64, channelLEDs [][]int) error {
	encoded, err := encodeChannelLEDs(channelLEDs)
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(`UPDATE wled_devices SET channel_leds = ? WHERE id = ?`, encoded, id)
	return err
}

// SetWLEDDeviceColours replaces the Red, Green, Blue, Yellow, and Strobe
// colours (in that, WLEDColourChannelLabels, order) for the WLED device with
// the given id. colours must have length WLEDColourChannelCount.
func SetWLEDDeviceColours(sqlDB *sql.DB, id int64, colours []uint32) error {
	if len(colours) != WLEDColourChannelCount {
		return fmt.Errorf("expected %d colours, got %d", WLEDColourChannelCount, len(colours))
	}
	_, err := sqlDB.Exec(`
		UPDATE wled_devices
		SET colour_red = ?, colour_green = ?, colour_blue = ?, colour_yellow = ?, colour_strobe = ?
		WHERE id = ?`,
		colours[0], colours[1], colours[2], colours[3], colours[4], id)
	return err
}
