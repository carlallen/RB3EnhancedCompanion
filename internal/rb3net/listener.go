package rb3net

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

// disconnectTimeout is how long we wait without any packet before
// considering RB3Enhanced disconnected. It sends an ALIVE packet
// periodically, so a longer silence means the game closed or the network
// link dropped.
const disconnectTimeout = 10 * time.Second

// Listener receives RB3Enhanced's UDP broadcast packets and applies decoded
// state to a Hub.
type Listener struct {
	Addr string // e.g. ":21070"
	Hub  *Hub

	mu       sync.Mutex
	lastSeen time.Time
}

func (l *Listener) Run(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", l.Addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	go l.watchdog(ctx)

	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			log.Printf("rb3net: read failed: %v", err)
			continue
		}
		l.handleDatagram(buf[:n], addr)
	}
}

// watchdog marks the hub disconnected after a period with no packets.
func (l *Listener) watchdog(ctx context.Context) {
	ticker := time.NewTicker(disconnectTimeout)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			idle := l.lastSeen.IsZero() || time.Since(l.lastSeen) >= disconnectTimeout
			l.mu.Unlock()
			if idle {
				l.Hub.Mutate(func(s *GameState) { s.Connected = false })
			}
		}
	}
}

func (l *Listener) handleDatagram(data []byte, addr *net.UDPAddr) {
	pkt, ok := parsePacket(data)
	if !ok {
		return
	}

	l.mu.Lock()
	l.lastSeen = time.Now()
	l.mu.Unlock()

	l.Hub.Mutate(func(s *GameState) {
		s.Connected = true
		s.Platform = pkt.Platform.String()
		s.SourceIP = addr.IP.String()

		switch pkt.Type {
		case ptAlive:
			// content is a build tag string; presence alone is enough to
			// mark the connection alive, nothing else to store today.
		case ptState:
			if len(pkt.Payload) >= 1 {
				s.InGame = pkt.Payload[0] != 0
			}
		case ptSongName:
			s.SongName = stringPayload(pkt.Payload)
		case ptSongArtist:
			s.SongArtist = stringPayload(pkt.Payload)
		case ptSongShortName:
			s.SongShortName = stringPayload(pkt.Payload)
		case ptVenueName:
			s.VenueName = stringPayload(pkt.Payload)
		case ptScreenName:
			s.ScreenName = stringPayload(pkt.Payload)
		case ptScore:
			if sc, ok := parseScore(pkt.Payload); ok {
				s.Score = Score(sc)
			}
		case ptBandInfo:
			if bi, ok := parseBandInfo(pkt.Payload); ok {
				for i := 0; i < 4; i++ {
					s.Band[i] = BandMember{
						Exists:     bi.Exists[i],
						Difficulty: bi.Difficulty[i],
						TrackType:  bi.TrackType[i],
					}
				}
			}
		case ptStagekit:
			if len(pkt.Payload) >= 2 {
				applyStagekit(&s.StageKit, pkt.Payload[0], pkt.Payload[1])
			}
		case ptDXData:
			// received but not surfaced yet
		}
	})
}

// applyStagekit updates the 8x4 LED grid from one STAGEKIT packet.
// ledMask is LeftChannel (bit per LED position); colour is RightChannel,
// a command selecting which colour group the mask applies to, or a
// fog/strobe/all-off command instead.
func applyStagekit(sk *StageKit, ledMask, colour byte) {
	switch colour {
	case skColourRed:
		sk.applyColour(0, ledMask)
	case skColourGreen:
		sk.applyColour(1, ledMask)
	case skColourBlue:
		sk.applyColour(2, ledMask)
	case skColourYellow:
		sk.applyColour(3, ledMask)
	case skStrobeSpeed1:
		sk.Strobe = 1
	case skStrobeSpeed2:
		sk.Strobe = 2
	case skStrobeSpeed3:
		sk.Strobe = 3
	case skStrobeSpeed4:
		sk.Strobe = 4
	case skStrobeOff, skAllOff:
		*sk = StageKit{}
	}
}
