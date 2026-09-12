package rb3net

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
)

// wledPort is the UDP port WLED listens for realtime pixel data on.
const wledPort = 21324

// wledWARLSProtocol selects WLED's WARLS realtime protocol: each LED to
// update is sent as its own (index, R, G, B) tuple, so a device's configured
// LEDs can be addressed directly - sparse, out of order, whatever the
// channel mapping calls for - rather than needing to fall within one
// contiguous run from a fixed start index the way DRGB-family protocols do.
// WARLS's LED index is a single byte, so it can only address LEDs 0-255.
const wledWARLSProtocol = 1

// wledTimeoutSeconds tells the WLED device how long to hold a frame before
// reverting to its own effects if no further packet arrives.
const wledTimeoutSeconds = 2

// wledCheckInterval is how often the watcher checks in on each device: if
// no new stage kit command has changed what should be showing since the
// last packet, the current frame is resent anyway, so a device's realtime
// hold never lapses before wledTimeoutSeconds runs out.
const wledCheckInterval = 1 * time.Second

// wledMaxLEDsPerPacket caps how many (index, R, G, B) entries a single WARLS
// packet carries, so a wide channel mapping doesn't produce a payload larger
// than a UDP packet's practical size.
const wledMaxLEDsPerPacket = 360

// wledJSONAPITimeout bounds a single JSON API request to a WLED device, so
// an unreachable device doesn't hold up turning off the others.
const wledJSONAPITimeout = 3 * time.Second

// wledStrobePeriod maps a Stage Kit strobe speed (1-4) to the duration of
// one full on/off cycle. WLED has no concept of the Stage Kit's strobe
// speeds, so this app drives the blink timing itself - matching the speeds
// shown by the web dashboard's own strobe indicator - sending WLED a
// literal on/off pixel for every transition rather than a static colour.
var wledStrobePeriod = [5]time.Duration{
	0,                      // off - unused
	900 * time.Millisecond, // speed 1
	600 * time.Millisecond, // speed 2
	400 * time.Millisecond, // speed 3
	250 * time.Millisecond, // speed 4
}

// wledFrame gives the colour every one of a device's configured LEDs
// should show for one instant of stage kit state.
type wledFrame map[int][3]byte

// buildWLEDFrame renders sk (with strobeLit giving the strobe pixel's
// current on/off phase) into physical LED colours, using d.ChannelLEDs to
// map each stage kit channel to the LED index(es) that mirror it and
// d.Colours for the colour shown for each of Red, Green, Blue, Yellow, and
// (while lit) Strobe.
func buildWLEDFrame(sk StageKit, strobeLit bool, d db.WLEDDevice) wledFrame {
	frame := make(wledFrame)
	for channel := 0; channel < db.WLEDChannelCount && channel < len(d.ChannelLEDs); channel++ {
		var colour [3]byte
		if channel == db.WLEDStrobeChannel {
			if strobeLit {
				colour = packedWLEDColour(d.Colours[db.WLEDStrobeColourIndex])
			}
		} else if pos, c := channel/4, channel%4; sk.LED[pos][c] {
			colour = packedWLEDColour(d.Colours[c])
		}
		for _, led := range d.ChannelLEDs[channel] {
			if led < 0 || led > 255 {
				continue
			}
			frame[led] = colour
		}
	}
	return frame
}

// packedWLEDColour unpacks one of WLEDDevice.Colours' 24-bit values into the
// [3]byte a wledFrame holds.
func packedWLEDColour(c uint32) [3]byte {
	r, g, b := db.UnpackWLEDColour(c)
	return [3]byte{r, g, b}
}

// sendWARLS sends frame to ip as one or more WLED WARLS realtime packets.
func sendWARLS(ip string, frame wledFrame) error {
	if len(frame) == 0 {
		return nil
	}

	leds := make([]int, 0, len(frame))
	for led := range frame {
		leds = append(leds, led)
	}

	conn, err := net.Dial("udp", net.JoinHostPort(ip, strconv.Itoa(wledPort)))
	if err != nil {
		return fmt.Errorf("dial %s: %w", ip, err)
	}
	defer conn.Close()

	for offset := 0; offset < len(leds); offset += wledMaxLEDsPerPacket {
		end := offset + wledMaxLEDsPerPacket
		if end > len(leds) {
			end = len(leds)
		}
		chunk := leds[offset:end]

		pkt := make([]byte, 2, 2+len(chunk)*4)
		pkt[0] = wledWARLSProtocol
		pkt[1] = wledTimeoutSeconds
		for _, led := range chunk {
			colour := frame[led]
			pkt = append(pkt, byte(led), colour[0], colour[1], colour[2])
		}
		if _, err := conn.Write(pkt); err != nil {
			return fmt.Errorf("write to %s: %w", ip, err)
		}
	}
	return nil
}

// setWLEDPower turns a WLED device fully on or off via its JSON API
// (POST /json/state), distinct from sendWARLS's realtime pixel frames: this
// takes the device out of realtime mode entirely, rather than just holding
// a black frame until wledTimeoutSeconds lapses and it falls back to its
// own configured effect.
func setWLEDPower(ip string, on bool) error {
	body, err := json.Marshal(map[string]bool{"on": on})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), wledJSONAPITimeout)
	defer cancel()

	target := fmt.Sprintf("http://%s/json/state", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to %s: %w", ip, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", ip, resp.Status)
	}
	return nil
}

// WLEDWatcher mirrors the stage kit's lights to every configured WLED
// device over UDP using WLED's WARLS realtime protocol. Frames are only
// sent while RB3Enhanced reports being in-game (GameState.InGame); outside
// of a song, WLED devices are left alone and fall back to their own
// configured effect once their realtime hold's wledTimeoutSeconds lapses.
// While in-game, frames are sent whenever the lights actually change, plus
// once every wledCheckInterval regardless - a check-in that resends the
// current frame when nothing new has arrived, keeping each device's
// realtime hold from lapsing. While the
// strobe is active, a separate timer flips it on and off at the Stage Kit's
// configured speed, sending a fresh frame on every transition. The device
// list is reloaded from the database periodically, so devices added,
// removed, or remapped on the config page take effect without a restart.
// Every enabled device is also explicitly turned on or off over its JSON
// API whenever RB3Enhanced's connection is gained or lost
// (GameState.Connected), rather than left to fall back to its own effect
// once wledTimeoutSeconds lapses after a disconnect.
type WLEDWatcher struct {
	Hub *Hub
	DB  *sql.DB

	mu      sync.RWMutex
	devices []db.WLEDDevice
}

func (w *WLEDWatcher) Run(ctx context.Context) {
	w.reloadDevices(ctx)

	stopReload := make(chan struct{})
	defer close(stopReload)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopReload:
				return
			case <-ticker.C:
				w.reloadDevices(ctx)
			}
		}
	}()

	ch := w.Hub.Subscribe()
	defer w.Hub.Unsubscribe(ch)

	initial := w.Hub.State()
	latest := initial.StageKit
	inGame := initial.InGame
	connected := initial.Connected
	strobeLit := false

	var strobeTicker *time.Ticker
	var strobeTickerC <-chan time.Time
	defer func() {
		if strobeTicker != nil {
			strobeTicker.Stop()
		}
	}()

	// rearmStrobe (re)starts the strobe timer to match latest.Strobe,
	// stopping it entirely when the strobe is off.
	rearmStrobe := func() {
		if strobeTicker != nil {
			strobeTicker.Stop()
			strobeTicker = nil
			strobeTickerC = nil
		}
		if latest.Strobe == 0 || int(latest.Strobe) >= len(wledStrobePeriod) {
			strobeLit = false
			return
		}
		strobeLit = true
		strobeTicker = time.NewTicker(wledStrobePeriod[latest.Strobe] / 2)
		strobeTickerC = strobeTicker.C
	}
	rearmStrobe()

	checkTicker := time.NewTicker(wledCheckInterval)
	defer checkTicker.Stop()

	var lastSentAt time.Time
	send := func(now time.Time) {
		if !inGame {
			return
		}
		w.broadcast(latest, strobeLit)
		lastSentAt = now
	}
	send(time.Now())

	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			prevStrobe := latest.Strobe
			latest = state.StageKit
			inGame = state.InGame
			if latest.Strobe != prevStrobe {
				rearmStrobe()
			}
			if state.Connected != connected {
				go w.setDevicesPower(state.Connected)
			}
			connected = state.Connected
			send(time.Now())
		case now := <-strobeTickerC:
			strobeLit = !strobeLit
			send(now)
		case now := <-checkTicker.C:
			if now.Sub(lastSentAt) >= wledCheckInterval {
				send(now)
			}
		}
	}
}

func (w *WLEDWatcher) reloadDevices(ctx context.Context) {
	devices, err := db.ListWLEDDevices(w.DB)
	if err != nil {
		log.Printf("wled: failed to load devices: %v", err)
		return
	}
	w.mu.Lock()
	w.devices = devices
	w.mu.Unlock()
}

// setDevicesPower sends every currently known enabled device an on/off
// command over its JSON API - called once on each edge of RB3Enhanced's
// connection, so a device doesn't sit showing its last frame until
// wledTimeoutSeconds lapses after a disconnect, and comes back on again as
// soon as RB3Enhanced reconnects rather than waiting for the next frame.
func (w *WLEDWatcher) setDevicesPower(on bool) {
	w.mu.RLock()
	devices := w.devices
	w.mu.RUnlock()

	for _, d := range devices {
		if !d.Enabled {
			continue
		}
		if err := setWLEDPower(d.IP, on); err != nil {
			log.Printf("wled: failed to set power for %s (%s): %v", d.IP, d.Name, err)
		}
	}
}

func (w *WLEDWatcher) broadcast(sk StageKit, strobeLit bool) {
	w.mu.RLock()
	devices := w.devices
	w.mu.RUnlock()

	for _, d := range devices {
		if !d.Enabled {
			continue
		}
		frame := buildWLEDFrame(sk, strobeLit, d)
		if err := sendWARLS(d.IP, frame); err != nil {
			log.Printf("wled: failed to send frame to %s (%s): %v", d.IP, d.Name, err)
		}
	}
}
