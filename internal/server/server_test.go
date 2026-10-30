package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amangeldievkuu/note-cli/internal/store"
)

const richBody = `## Shopping list

| item | qty |
|------|-----|
| milk | 2   |

- [x] bread
- [ ] eggs

` + "```go\nfunc main() {}\n```" + `

Fresh produce matters[^1].

[^1]: Says everyone.
`

func newTestServer(t *testing.T, write bool) (*httptest.Server, *Server, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "notes"))
	if err != nil {
		t.Fatal(err)
	}
	// A fixed clock makes every note equally recent, so the index lists
	// them in ID order instead of depending on a second boundary.
	fixed := time.Date(2026, 10, 29, 10, 15, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return fixed })
	for _, n := range []struct {
		title string
		tags  []string
		body  string
		done  bool
	}{
		{"Buy groceries", []string{"errands"}, richBody, false},
		{"Old task", nil, "done already\n", true},
		{"Work thing", []string{"work"}, "meeting\n", false},
	} {
		created, err := s.Create(n.title, n.tags, n.body)
		if err != nil {
			t.Fatal(err)
		}
		if n.done {
			if _, err := s.SetDone(created.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	srv, err := New(s, Options{Write: write})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv, s
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestIndex(t *testing.T) {
	ts, _, _ := newTestServer(t, false)
	tests := []struct {
		query   string
		want    []string // in this order
		notWant []string
	}{
		{
			query: "",
			want:  []string{"Buy groceries", "Work thing", `<li class="done"`, "Old task"},
		},
		{query: "?status=open", want: []string{"Buy groceries", "Work thing"}, notWant: []string{"Old task"}},
		{query: "?status=done", want: []string{"Old task"}, notWant: []string{"Buy groceries", "Work thing"}},
		{query: "?tag=work", want: []string{"Work thing"}, notWant: []string{"Buy groceries", "Old task"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			code, body := get(t, ts.URL+"/"+tt.query)
			if code != http.StatusOK {
				t.Fatalf("status %d", code)
			}
			at := 0
			for _, w := range tt.want {
				i := strings.Index(body[at:], w)
				if i < 0 {
					t.Fatalf("missing %q after offset %d in:\n%s", w, at, body)
				}
				at += i + len(w)
			}
			for _, w := range tt.notWant {
				if strings.Contains(body, `">`+w+`</a>`) {
					t.Errorf("unexpected %q", w)
				}
			}
		})
	}
}

func TestIndexOpenNotesFirst(t *testing.T) {
	ts, _, s := newTestServer(t, false)
	// Touch the done note so it is the most recently updated.
	n, _ := s.Get(2)
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, ts.URL+"/")
	if strings.Index(body, "Old task") < strings.Index(body, "Work thing") {
		t.Error("done note listed before an open note")
	}
}

func TestNotePage(t *testing.T) {
	ts, _, _ := newTestServer(t, false)
	code, body := get(t, ts.URL+"/n/1")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{
		"<title>Buy groceries · note</title>",
		`<h1>Buy groceries</h1>`,
		`href="/?status=all&amp;tag=errands"`,
		`badge-open`,
		`<table>`,
		`<input checked="" disabled="" type="checkbox"`,
		`<pre class="chroma">`,
		`<h2 id="shopping-list">Shopping list<a href="#shopping-list" class="anchor">#</a></h2>`,
		`class="footnotes"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("note page missing %s", want)
		}
	}
	if strings.Contains(body, "<form") {
		t.Error("read-only page has forms")
	}

	_, body = get(t, ts.URL+"/n/2")
	if !strings.Contains(body, `<h1 class="done">`) || !strings.Contains(body, "badge-done") {
		t.Error("done note is not marked done")
	}
}

func TestNotFoundAndBadRequests(t *testing.T) {
	ts, _, _ := newTestServer(t, false)
	tests := []struct {
		path string
		code int
	}{
		{"/n/99", http.StatusNotFound},
		{"/n/abc", http.StatusBadRequest},
		{"/nope", http.StatusNotFound},
		{"/static/style.css", http.StatusOK},
		{"/static/chroma.css", http.StatusOK},
		{"/static/app.js", http.StatusOK},
	}
	for _, tt := range tests {
		if code, body := get(t, ts.URL+tt.path); code != tt.code {
			t.Errorf("GET %s = %d, want %d (%s)", tt.path, code, tt.code, body)
		}
	}
}

func TestForeignHostRejected(t *testing.T) {
	ts, _, _ := newTestServer(t, false)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Host = "evil.example:7777"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func post(t *testing.T, target string, form url.Values, header http.Header) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := noRedirect().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestReadOnlyHasNoPostRoutes(t *testing.T) {
	ts, srv, s := newTestServer(t, false)
	code := post(t, ts.URL+"/n/1/done", url.Values{"csrf": {srv.csrf}}, nil)
	if code < 400 {
		t.Errorf("POST on read-only server = %d, want an error", code)
	}
	if n, _ := s.Get(1); n.Done {
		t.Error("note marked done on a read-only server")
	}
}

func TestWriteActions(t *testing.T) {
	ts, srv, s := newTestServer(t, true)

	_, body := get(t, ts.URL+"/n/1")
	if !strings.Contains(body, `name="csrf" value="`+srv.csrf+`"`) {
		t.Fatal("note page has no CSRF token")
	}

	tests := []struct {
		name   string
		path   string
		form   url.Values
		header http.Header
		code   int
		check  func(t *testing.T)
	}{
		{
			name: "missing token", path: "/n/1/done", code: http.StatusForbidden,
			check: func(t *testing.T) { wantDone(t, s, 1, false) },
		},
		{
			name: "wrong token", path: "/n/1/done", form: url.Values{"csrf": {"nope"}}, code: http.StatusForbidden,
			check: func(t *testing.T) { wantDone(t, s, 1, false) },
		},
		{
			name: "cross-site request", path: "/n/1/done", form: url.Values{"csrf": {srv.csrf}},
			header: http.Header{"Sec-Fetch-Site": {"cross-site"}, "Origin": {"https://evil.example"}},
			code:   http.StatusForbidden,
			check:  func(t *testing.T) { wantDone(t, s, 1, false) },
		},
		{
			name: "mark done", path: "/n/1/done", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusSeeOther,
			check: func(t *testing.T) { wantDone(t, s, 1, true) },
		},
		{
			name: "mark open", path: "/n/2/undone", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusSeeOther,
			check: func(t *testing.T) { wantDone(t, s, 2, false) },
		},
		{
			name: "start", path: "/n/1/start", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusSeeOther,
			check: func(t *testing.T) { wantDoing(t, s, 1, true) },
		},
		{
			name: "stop", path: "/n/1/stop", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusSeeOther,
			check: func(t *testing.T) { wantDoing(t, s, 1, false) },
		},
		{
			name: "delete", path: "/n/3/delete", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusSeeOther,
			check: func(t *testing.T) {
				if _, err := s.Get(3); err == nil {
					t.Error("note 3 still exists")
				}
				if trashed, _ := s.ListTrash(); len(trashed) != 1 || trashed[0].ID != 3 {
					t.Error("note 3 not in trash")
				}
			},
		},
		{name: "missing note", path: "/n/99/done", form: url.Values{"csrf": {srv.csrf}}, code: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := post(t, ts.URL+tt.path, tt.form, tt.header); code != tt.code {
				t.Errorf("POST %s = %d, want %d", tt.path, code, tt.code)
			}
			if tt.check != nil {
				tt.check(t)
			}
		})
	}
}

func wantDone(t *testing.T, s *store.Store, id int, done bool) {
	t.Helper()
	n, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Done != done {
		t.Errorf("note %d done = %v, want %v", id, n.Done, done)
	}
}

func wantDoing(t *testing.T, s *store.Store, id int, doing bool) {
	t.Helper()
	n, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Doing != doing {
		t.Errorf("note %d doing = %v, want %v", id, n.Doing, doing)
	}
}

func TestDoingNote(t *testing.T) {
	ts, _, s := newTestServer(t, true)
	if _, err := s.SetDoing(3, true); err != nil {
		t.Fatal(err)
	}

	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, `<li class="doing"`) || !strings.Contains(body, `class="dot"`) {
		t.Error("doing note has no dot in the index")
	}
	if strings.Index(body, "Work thing") > strings.Index(body, "Buy groceries") {
		t.Error("doing note not listed first")
	}

	_, body = get(t, ts.URL+"/n/3")
	if !strings.Contains(body, "badge-doing") || !strings.Contains(body, `action="/n/3/stop"`) {
		t.Error("doing note page has no doing badge or Stop button")
	}
	_, body = get(t, ts.URL+"/n/1")
	if !strings.Contains(body, `action="/n/1/start"`) {
		t.Error("open note page has no Start button")
	}
}

func TestHealth(t *testing.T) {
	ts, _, s := newTestServer(t, false)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.App != "note" || h.NotesDir != s.Dir() || h.PID != os.Getpid() {
		t.Errorf("health = %+v", h)
	}
	if !Probe(context.Background(), ts.URL, s.Dir()) {
		t.Error("Probe = false for the right dir")
	}
	if Probe(context.Background(), ts.URL, "/some/other/dir") {
		t.Error("Probe = true for another dir")
	}
}

// TestLiveReload checks the whole path: a note changes on disk, the watcher
// notices, and an open event stream receives "reload".
func TestLiveReload(t *testing.T) {
	ts, srv, s := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.watch(ctx); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	lines := bufio.NewScanner(resp.Body)
	waitFor := func(want string) {
		t.Helper()
		for lines.Scan() {
			if lines.Text() == want {
				return
			}
		}
		t.Fatalf("stream ended before %q: %v", want, lines.Err())
	}
	waitFor(": connected")

	// Store bookkeeping files alone must not trigger a reload, but a note
	// edit must.
	if err := os.WriteFile(filepath.Join(s.Dir(), ".nextid"), []byte("99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDone(1, true); err != nil {
		t.Fatal(err)
	}
	waitFor("event: reload")
}

func TestIsNoteFile(t *testing.T) {
	tests := map[string]bool{
		"/n/0001-a.md":     true,
		"/n/.tmp-123":      false,
		"/n/.nextid":       false,
		"/n/0001-a.md.swp": false,
		"/n/4913":          false,
		"/n/0001-a.md~":    false,
	}
	for path, want := range tests {
		if got := isNoteFile(path); got != want {
			t.Errorf("isNoteFile(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestListenFallsBack(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port

	l, err := Listen(port)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	if addr.Port <= port || addr.Port >= port+20 || !addr.IP.IsLoopback() {
		t.Errorf("Listen(%d) bound %s, want 127.0.0.1 on a later port", port, addr)
	}
}

func TestIndexSearchesWholeBody(t *testing.T) {
	ts, _, s := newTestServer(t, false)
	if _, err := s.Create("Long note", nil, strings.Repeat("filler ", 200)+"needle\n"); err != nil {
		t.Fatal(err)
	}
	_, body := get(t, ts.URL+"/")
	if !strings.Contains(body, "needle") {
		t.Error("search text is missing a word deep in the body")
	}
}

func TestIndexTagBarCountsAllNotes(t *testing.T) {
	ts, _, _ := newTestServer(t, false)
	// Filtering by one tag still lists every tag in the bar.
	_, body := get(t, ts.URL+"/?tag=work")
	for _, w := range []string{"#errands", "#work"} {
		if !strings.Contains(body, w) {
			t.Errorf("tag bar missing %s", w)
		}
	}
}
