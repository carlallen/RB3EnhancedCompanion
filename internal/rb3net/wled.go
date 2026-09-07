package rb3net

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
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

// wledColour is the colour shown on a WLED pixel while its corresponding
// stage kit colour is lit.
var wledColour = [4][3]byte{
	{255, 0, 0},   // Red
	{0, 255, 0},   // Green
	{0, 0, 255},   // Blue
	{255, 200, 0}, // Yellow
}

// wledFrame gives the colour every one of a device's configured LEDs
// should show for one instant of stage kit state.
type wledFrame map[int][3]byte

// buildWLEDFrame renders sk (with strobeLit giving the strobe pixel's
// current on/off phase) into physical LED colours, using channelLEDs (see
// db.WLEDDevice.ChannelLEDs) to map each stage kit channel to the LED
// index(es) that mirror it.
func buildWLEDFrame(sk StageKit, strobeLit bool, channelLEDs [][]int) wledFrame {
	frame := make(wledFrame)
	for channel := 0; channel < db.WLEDChannelCount && channel < len(channelLEDs); channel++ {
		var colour [3]byte
		if channel == db.WLEDStrobeChannel {
			if strobeLit {
				colour = [3]byte{255, 255, 255}
			}
		} else if pos, c := channel/4, channel%4; sk.LED[pos][c] {
			colour = wledColour[c]
		}
		for _, led := range channelLEDs[channel] {
			if led < 0 || led > 255 {
				continue
			}
			frame[led] = colour
		}
	}
	return frame
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

// WLEDWatcher mirrors the stage kit's lights to every configured WLED
// device over UDP using WLED's WARLS realtime protocol. Frames are sent
// whenever the lights actually change, plus once every wledCheckInterval
// regardless - a check-in that resends the current frame when nothing new
// has arrived, keeping each device's realtime hold from lapsing. While the
// strobe is active, a separate timer flips it on and off at the Stage Kit's
// configured speed, sending a fresh frame on every transition. The device
// list is reloaded from the database periodically, so devices added,
// removed, or remapped on the config page take effect without a restart.
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

	latest := w.Hub.State().StageKit
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
			if latest.Strobe != prevStrobe {
				rearmStrobe()
			}
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

func (w *WLEDWatcher) broadcast(sk StageKit, strobeLit bool) {
	w.mu.RLock()
	devices := w.devices
	w.mu.RUnlock()

	for _, d := range devices {
		if !d.Enabled {
			continue
		}
		frame := buildWLEDFrame(sk, strobeLit, d.ChannelLEDs)
		if err := sendWARLS(d.IP, frame); err != nil {
			log.Printf("wled: failed to send frame to %s (%s): %v", d.IP, d.Name, err)
		}
	}
}
