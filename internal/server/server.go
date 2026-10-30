// Package server serves the notes as a local, live-reloading web site.
package server

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amangeldievkuu/note-cli/internal/render"
	"github.com/amangeldievkuu/note-cli/internal/store"
	"github.com/amangeldievkuu/note-cli/web"
)

// Options configure a Server.
type Options struct {
	// Write enables the "Mark done", "Start", and "Delete" buttons and
	// their POST endpoints. The server is read-only without it.
	Write bool
}

// Server renders notes from a store.
type Server struct {
	store *store.Store
	write bool
	csrf  string
	hub   *hub
	now   func() time.Time
	index *template.Template
	note  *template.Template
}

// New returns a Server for s.
func New(s *store.Store, opts Options) (*Server, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	srv := &Server{store: s, write: opts.Write, csrf: hex.EncodeToString(token), hub: newHub(), now: time.Now}
	funcs := template.FuncMap{
		"reltime": func(t time.Time) string { return render.RelTime(t, srv.now()) },
		"date":    func(t time.Time) string { return t.Local().Format("Jan 2, 2006 15:04") },
		"iso":     func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
	}
	parse := func(page string) (*template.Template, error) {
		return template.New("layout.html").Funcs(funcs).ParseFS(web.FS, "templates/layout.html", "templates/"+page)
	}
	var err error
	if srv.index, err = parse("index.html"); err != nil {
		return nil, err
	}
	if srv.note, err = parse("note.html"); err != nil {
		return nil, err
	}
	return srv, nil
}

// Handler returns the HTTP handler with every route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /n/{id}", s.handleNote)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /static/chroma.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(render.ChromaCSS())
	})
	mux.Handle("GET /static/", http.FileServerFS(web.FS))

	if s.write {
		cop := http.NewCrossOriginProtection()
		for path, set := range map[string]func(id int) (*store.Note, error){
			"done":   func(id int) (*store.Note, error) { return s.store.SetDone(id, true) },
			"undone": func(id int) (*store.Note, error) { return s.store.SetDone(id, false) },
			"start":  func(id int) (*store.Note, error) { return s.store.SetDoing(id, true) },
			"stop":   func(id int) (*store.Note, error) { return s.store.SetDoing(id, false) },
		} {
			mux.Handle("POST /n/{id}/"+path, cop.Handler(s.csrfCheck(s.handleSet(set))))
		}
		mux.Handle("POST /n/{id}/delete", cop.Handler(s.csrfCheck(http.HandlerFunc(s.handleDelete))))
	}
	return localOnly(logRequests(mux))
}

// localOnly rejects requests whose Host isn't a loopback name. Binding to
// 127.0.0.1 isn't enough on its own: a web page on a domain that resolves
// to 127.0.0.1 (DNS rebinding) could otherwise read notes.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		switch strings.Trim(host, "[]") {
		case "127.0.0.1", "localhost", "::1":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "forbidden host", http.StatusForbidden)
		}
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

func (s *Server) csrfCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(s.csrf)) != 1 {
			http.Error(w, "invalid or missing CSRF token; reload the page and try again", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type tab struct {
	Status, Label string
	Count         int
	Active        bool
}

type listItem struct {
	*store.Note
	Search string
}

type indexPage struct {
	Notes  []listItem
	Tabs   []tab
	Tags   []store.TagCount
	Status string
	Tag    string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "open" && status != "done" {
		status = "all"
	}
	tag := strings.TrimPrefix(r.URL.Query().Get("tag"), "#")

	// One pass over the notes serves both the tag bar and the list.
	notes, err := s.store.List(store.Filter{Status: store.StatusAll})
	if err != nil {
		s.serverError(w, err)
		return
	}
	all := notes
	if tag != "" {
		all = slices.DeleteFunc(slices.Clone(notes), func(n *store.Note) bool { return !n.HasTag(tag) })
	}

	page := indexPage{Status: status, Tag: tag, Tags: store.CountTags(notes)}
	open := 0
	for _, n := range all {
		if !n.Done {
			open++
		}
		if status == "open" && n.Done || status == "done" && !n.Done {
			continue
		}
		page.Notes = append(page.Notes, listItem{Note: n, Search: searchText(n)})
	}
	// Doing notes first, then open, then done; most recently updated first
	// in each.
	slices.SortStableFunc(page.Notes, func(a, b listItem) int {
		return cmp.Or(cmp.Compare(rank(a.Note), rank(b.Note)), b.Updated.Compare(a.Updated))
	})
	page.Tabs = []tab{
		{Status: "open", Label: "Open", Count: open, Active: status == "open"},
		{Status: "done", Label: "Done", Count: len(all) - open, Active: status == "done"},
		{Status: "all", Label: "All", Count: len(all), Active: status == "all"},
	}
	s.renderPage(w, s.index, page)
}

// rank orders notes in the index: doing, open, done.
func rank(n *store.Note) int {
	switch {
	case n.Doing:
		return 0
	case n.Done:
		return 2
	}
	return 1
}

// searchText is what the client-side search box matches against: the
// whole note, so matches deep in a long body are found too.
func searchText(n *store.Note) string {
	return strings.ToLower(n.Title + " " + strings.Join(n.Tags, " ") + " " + n.Body)
}

type notePage struct {
	Note  *store.Note
	Body  template.HTML
	Write bool
	CSRF  string
}

func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	n, ok := s.lookup(w, r)
	if !ok {
		return
	}
	body, err := render.HTML(n.Body)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, s.note, notePage{Note: n, Body: body, Write: s.write, CSRF: s.csrf})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*store.Note, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.Error(w, "bad note id", http.StatusBadRequest)
		return nil, false
	}
	n, err := s.store.Get(id)
	if _, notFound := errors.AsType[*store.NotFoundError](err); notFound {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		s.serverError(w, err)
		return nil, false
	}
	return n, true
}

// handleSet applies set to the note and redirects back to its page.
func (s *Server) handleSet(set func(id int) (*store.Note, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, ok := s.lookup(w, r)
		if !ok {
			return
		}
		if _, err := set(n.ID); err != nil {
			s.serverError(w, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/n/%d", n.ID), http.StatusSeeOther)
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	n, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if _, err := s.store.Trash(n.ID); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Health is the body of GET /healthz. "note open" uses it to find a
// running server for the same notes directory.
type Health struct {
	App      string `json:"app"`
	NotesDir string `json:"notes_dir"`
	PID      int    `json:"pid"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Health{App: "note", NotesDir: s.store.Dir(), PID: os.Getpid()})
}

func (s *Server) renderPage(w http.ResponseWriter, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		s.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	buf.WriteTo(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// Listen binds 127.0.0.1 on port, or on the next free port within 20 if
// it is taken.
func Listen(port int) (net.Listener, error) {
	var firstErr error
	for p := port; p < port+20 && p <= 65535; p++ {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err == nil {
			return l, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, fmt.Errorf("no free port from %d to %d: %w", port, port+19, firstErr)
}

// Serve serves on l and watches the notes directory for live reload until
// ctx is canceled.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	if err := s.watch(ctx); err != nil {
		slog.Warn("live reload disabled", "err", err)
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// Request contexts end with ctx, so open event streams close on
		// shutdown instead of holding it up.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
