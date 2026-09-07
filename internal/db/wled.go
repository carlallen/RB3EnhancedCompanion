package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// WLEDDevice is a WLED-powered LED strip that can be mapped to mirror the
// stage kit's lights. Nothing yet sends data to a configured device - this
// is configuration storage only.
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

const createWLEDDevicesTable = `
CREATE TABLE IF NOT EXISTS wled_devices (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	name         TEXT NOT NULL DEFAULT '',
	ip           TEXT NOT NULL,
	channel_leds TEXT NOT NULL DEFAULT '',
	enabled      INTEGER NOT NULL DEFAULT 1
)`

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
	rows, err := sqlDB.Query(`SELECT id, name, ip, channel_leds, enabled FROM wled_devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []WLEDDevice
	for rows.Next() {
		var d WLEDDevice
		var channelLEDs string
		if err := rows.Scan(&d.ID, &d.Name, &d.IP, &channelLEDs, &d.Enabled); err != nil {
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
