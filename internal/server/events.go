package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	debounce  = 150 * time.Millisecond
	heartbeat = 25 * time.Second
)

// hub fans change notifications out to connected event streams.
type hub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newHub() *hub { return &hub{subs: map[chan struct{}]struct{}{}} }

func (h *hub) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// broadcast notifies every subscriber without blocking; a subscriber that
// already has a pending notification doesn't need a second one.
func (h *hub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// handleEvents streams Server-Sent Events: "reload" whenever a note file
// changes, plus a comment line as a heartbeat.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch, unsubscribe := s.hub.subscribe()
	defer unsubscribe()

	if _, err := fmt.Fprint(w, "retry: 1000\n: connected\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		var msg string
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			msg = "event: reload\ndata: {}\n\n"
		case <-tick.C:
			msg = ": ping\n\n"
		}
		if _, err := fmt.Fprint(w, msg); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// watch notifies the hub when a note file in the notes directory changes,
// until ctx is canceled. Watching the directory rather than each file
// catches editors that save by writing a new file and renaming it.
func (s *Server) watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(s.store.Dir()); err != nil {
		w.Close()
		return err
	}
	go func() {
		defer w.Close()
		timer := time.NewTimer(debounce)
		timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if isNoteFile(ev.Name) {
					slog.Debug("note changed", "file", ev.Name, "op", ev.Op)
					timer.Reset(debounce)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Warn("watching notes dir", "err", err)
			case <-timer.C:
				s.hub.broadcast()
			}
		}
	}()
	return nil
}

// isNoteFile reports whether a changed path is a note, skipping temp files,
// editor swap files, and store bookkeeping.
func isNoteFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".md") && !strings.HasPrefix(base, ".")
}
