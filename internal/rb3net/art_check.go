package rb3net

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/carlallen/RB3EnhancedCompanion/internal/db"
)

// artURLTemplate is the RB3CE-db repository's raw-content URL for a song's
// bundled album art, keyed by origin and shortname.
const artURLTemplate = "https://github.com/carlallen/RB3CE-db/blob/main/art/%s/%s_keep.png?raw=true"

// ArtCheckWatcher checks every song not yet checked (db.PendingArtCheckSongs)
// against the RB3CE-db art repository each time the song list is (re)loaded -
// both the initial load from the database at startup and every subsequent
// console fetch bump SongList and so wake this watcher, since it just
// watches for SongList changes on the Hub rather than being wired into
// either load path directly. Any art found is downloaded into
// storeDir/art; each song is then marked checked so it isn't retried.
type ArtCheckWatcher struct {
	Hub      *Hub
	DB       *sql.DB
	StoreDir string

	// mu guards running and rerun, which together coalesce version bumps
	// that arrive while a check() pass is still in flight (see trigger) so
	// two passes never run concurrently - both would pull overlapping
	// PendingArtCheckSongs results and race on downloadArt's shared tmp
	// file for any song they have in common.
	mu      sync.Mutex
	running bool
	rerun   bool
}

func (w *ArtCheckWatcher) Run(ctx context.Context) {
	ch := w.Hub.Subscribe()
	defer w.Hub.Unsubscribe(ch)

	// The startup load into the Hub (see cmd/server/main.go) happens before
	// this watcher subscribes, so it wouldn't otherwise see that version -
	// check the Hub's current state once up front to cover it.
	lastVersion := -1
	if state := w.Hub.State(); state.SongListVersion != lastVersion {
		lastVersion = state.SongListVersion
		w.trigger(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-ch:
			if !ok {
				return
			}
			if state.SongListVersion == lastVersion {
				continue
			}
			lastVersion = state.SongListVersion
			w.trigger(ctx)
		}
	}
}

// trigger starts a check() pass, unless one is already running - in which
// case it flags that pass to run again once it finishes, rather than
// starting a second pass concurrently.
func (w *ArtCheckWatcher) trigger(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.rerun = true
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		for {
			w.check(ctx)

			w.mu.Lock()
			if !w.rerun || ctx.Err() != nil {
				w.running = false
				w.rerun = false
				w.mu.Unlock()
				return
			}
			w.rerun = false
			w.mu.Unlock()
		}
	}()
}

func (w *ArtCheckWatcher) check(ctx context.Context) {
	pending, err := db.PendingArtCheckSongs(w.DB)
	if err != nil {
		log.Printf("rb3net: art check: failed to load pending songs: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	artDir := filepath.Join(w.StoreDir, "art")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		log.Printf("rb3net: art check: failed to create %s: %v", artDir, err)
		return
	}

	for _, s := range pending {
		if ctx.Err() != nil {
			return
		}

		if err := downloadArt(ctx, artDir, s.Shortname, s.Origin); err != nil {
			// A transient failure (network, disk, unexpected status) - leave
			// db_art_check false so this song is retried on the next check
			// rather than being skipped forever.
			log.Printf("rb3net: art check: %s: %v", s.Shortname, err)
			continue
		}

		if err := db.MarkArtChecked(w.DB, s.Shortname); err != nil {
			log.Printf("rb3net: art check: failed to mark %s checked: %v", s.Shortname, err)
		}
	}
}

// downloadArt checks the RB3CE-db repository for shortname's art and, if
// present, downloads it into artDir. A 404 (no art for this song) is not an
// error - err is only set for a failed check.
func downloadArt(ctx context.Context, artDir, shortname, origin string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	target := fmt.Sprintf(artURLTemplate, url.PathEscape(origin), url.PathEscape(shortname))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	dest := filepath.Join(artDir, shortname+"_keep.png")
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}
