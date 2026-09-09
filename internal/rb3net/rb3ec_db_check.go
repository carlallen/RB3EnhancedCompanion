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

// artURLTemplate is the RB3EC-db repository's raw-content URL for a song's
// bundled album art, keyed by origin and shortname.
const artURLTemplate = "https://github.com/carlallen/RB3EC-db/blob/main/art/%s/%s_keep.png?raw=true"

// metadataURLTemplate is the RB3EC-db repository's raw-content URL for a
// song's bundled metadata, keyed by origin and shortname.
const metadataURLTemplate = "https://raw.githubusercontent.com/carlallen/RB3EC-db/refs/heads/main/metadata/%s/%s.yml"

// RB3ECDBCheckWatcher checks every song not yet checked
// (db.PendingRB3ECDBCheckSongs) against the RB3EC-db repository each time the
// song list is (re)loaded - both the initial load from the database at
// startup and every subsequent console fetch bump SongList and so wake this
// watcher, since it just watches for SongList changes on the Hub rather than
// being wired into either load path directly. For each such song: if it has
// no art locally yet, art is fetched from RB3EC-db and, if present,
// downloaded into storeDir/art; independently, if it has no local metadata
// file (checked via MetadataDirs), metadata is fetched from RB3EC-db and, if
// present, imported directly into the song's row (see db.ImportMetadata).
// The song is then marked checked so it isn't retried.
type RB3ECDBCheckWatcher struct {
	Hub      *Hub
	DB       *sql.DB
	StoreDir string

	// MetadataDirs is checked, in order, for a local <shortname>.yml
	// metadata file - the same list passed to db.SaveSongs. A song with no
	// local file has its metadata fetched from RB3EC-db and imported
	// directly into the database.
	MetadataDirs []string

	// mu guards running and rerun, which together coalesce version bumps
	// that arrive while a check() pass is still in flight (see trigger) so
	// two passes never run concurrently - both would pull overlapping
	// PendingRB3ECDBCheckSongs results and race on downloadArt's shared tmp
	// file for any song they have in common.
	mu      sync.Mutex
	running bool
	rerun   bool
}

func (w *RB3ECDBCheckWatcher) Run(ctx context.Context) {
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
func (w *RB3ECDBCheckWatcher) trigger(ctx context.Context) {
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

func (w *RB3ECDBCheckWatcher) check(ctx context.Context) {
	pending, err := db.PendingRB3ECDBCheckSongs(w.DB)
	if err != nil {
		log.Printf("rb3net: RB3EC db check: failed to load pending songs: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	artDir := filepath.Join(w.StoreDir, "art")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		log.Printf("rb3net: RB3EC db check: failed to create %s: %v", artDir, err)
		return
	}

	for _, s := range pending {
		if ctx.Err() != nil {
			return
		}

		// A transient failure fetching either (network, disk, unexpected
		// status) leaves RB3EC_db_check false so this song is retried on the
		// next check rather than being skipped forever.
		ok := true

		if !hasLocalArt(artDir, s.Shortname) {
			if err := downloadArt(ctx, artDir, s.Shortname, s.Origin); err != nil {
				log.Printf("rb3net: art check: %s: %v", s.Shortname, err)
				ok = false
			}
		}

		if !hasLocalMetadata(s.Shortname, w.MetadataDirs) {
			if err := importRemoteMetadata(ctx, w.DB, s.Shortname, s.Origin); err != nil {
				log.Printf("rb3net: metadata check: %s: %v", s.Shortname, err)
				ok = false
			}
		}

		if !ok {
			continue
		}

		if err := db.MarkRB3ECDBChecked(w.DB, s.Shortname); err != nil {
			log.Printf("rb3net: RB3EC db check: failed to mark %s checked: %v", s.Shortname, err)
		}
	}
}

// hasLocalArt reports whether shortname's custom album art has already been
// downloaded into artDir.
func hasLocalArt(artDir, shortname string) bool {
	info, err := os.Stat(filepath.Join(artDir, shortname+"_keep.png"))
	return err == nil && !info.IsDir()
}

// hasLocalMetadata reports whether a <shortname>.yml metadata file already
// exists in any of metadataDirs.
func hasLocalMetadata(shortname string, metadataDirs []string) bool {
	for _, dir := range metadataDirs {
		if dir == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, shortname+".yml"))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// downloadArt checks the RB3EC-db repository for shortname's art and, if
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

// importRemoteMetadata checks the RB3EC-db repository for shortname's
// metadata and, if present, imports it directly into the song's row (see
// db.ImportMetadata) - it's never written to disk. A 404 (no metadata for
// this song) is not an error - err is only set for a failed check.
func importRemoteMetadata(ctx context.Context, sqlDB *sql.DB, shortname, origin string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	target := fmt.Sprintf(metadataURLTemplate, url.PathEscape(origin), url.PathEscape(shortname))
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return db.ImportMetadata(sqlDB, shortname, data)
}
