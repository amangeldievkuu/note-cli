package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 29, 10, 15, 0, 0, time.UTC)

// newStore returns a store in a temp dir whose clock advances one minute
// per call.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := t0
	s.SetClock(func() time.Time {
		now = now.Add(time.Minute)
		return now
	})
	return s
}

type seed struct {
	title string
	tags  []string
	body  string
	done  bool
}

func seedStore(t *testing.T, s *Store, notes ...seed) []*Note {
	t.Helper()
	var out []*Note
	for _, sd := range notes {
		n, err := s.Create(sd.title, sd.tags, sd.body)
		if err != nil {
			t.Fatal(err)
		}
		if sd.done {
			if n, err = s.SetDone(n.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		out = append(out, n)
	}
	return out
}

func ids(notes []*Note) []int {
	out := make([]int, len(notes))
	for i, n := range notes {
		out[i] = n.ID
	}
	return out
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		tags      []string
		body      string
		wantTitle string
		wantTags  []string
		wantFile  string
	}{
		{
			name:      "title and tags",
			title:     "Buy groceries",
			tags:      []string{"personal", " Errands ", "personal"},
			body:      "- [ ] milk\n",
			wantTitle: "Buy groceries",
			wantTags:  []string{"personal", "errands"},
			wantFile:  "0001-buy-groceries.md",
		},
		{
			name:      "title from body heading",
			body:      "# Meeting notes\n\nstuff\n",
			wantTitle: "Meeting notes",
			wantTags:  []string{},
			wantFile:  "0001-meeting-notes.md",
		},
		{
			name:      "empty",
			wantTitle: "Untitled",
			wantTags:  []string{},
			wantFile:  "0001-untitled.md",
		},
		{
			name:      "punctuation slug",
			title:     "  Fix: the *bike*!  ",
			wantTitle: "Fix: the *bike*!",
			wantTags:  []string{},
			wantFile:  "0001-fix-the-bike.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore(t)
			n, err := s.Create(tt.title, tt.tags, tt.body)
			if err != nil {
				t.Fatal(err)
			}
			if n.ID != 1 || n.Title != tt.wantTitle || !slices.Equal(n.Tags, tt.wantTags) {
				t.Errorf("Create = id %d title %q tags %q, want 1 %q %q", n.ID, n.Title, n.Tags, tt.wantTitle, tt.wantTags)
			}
			if got := filepath.Base(n.Path); got != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}

			got, err := s.Get(1)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != n.Title || !slices.Equal(got.Tags, n.Tags) || got.Body != n.Body ||
				!got.Created.Equal(n.Created) || !got.Updated.Equal(n.Updated) || got.Done {
				t.Errorf("round trip:\n got %+v\nwant %+v", got, n)
			}
		})
	}
}

func TestFileFormat(t *testing.T) {
	s := newStore(t)
	n, err := s.Create("Buy groceries", []string{"personal", "errands"}, "- [ ] milk")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := `---
id: 1
title: Buy groceries
tags: [personal, errands]
done: false
created: 2026-10-29T10:16:00Z
updated: 2026-10-29T10:16:00Z
---

- [ ] milk
`
	if string(data) != want {
		t.Errorf("file contents:\n%s\nwant:\n%s", data, want)
	}
}

func TestParseNote(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantTitle string
		wantBody  string
		wantLine  int
		wantErr   bool
	}{
		{name: "frontmatter", in: "---\ntitle: A\n---\n\nbody\n", wantTitle: "A", wantBody: "body\n", wantLine: 5},
		{name: "no blank line", in: "---\ntitle: A\n---\nbody", wantTitle: "A", wantBody: "body\n", wantLine: 4},
		{name: "crlf", in: "---\r\ntitle: A\r\n---\r\n\r\nbody\r\n", wantTitle: "A", wantBody: "body\n", wantLine: 5},
		{name: "empty body", in: "---\ntitle: A\n---\n", wantTitle: "A", wantBody: "", wantLine: 4},
		{name: "fence at eof", in: "---\ntitle: A\n---", wantTitle: "A", wantBody: "", wantLine: 3},
		{name: "empty frontmatter", in: "---\n---\n# Hi\n", wantTitle: "Hi", wantBody: "# Hi\n", wantLine: 3},
		{name: "no frontmatter", in: "\n# Plain file\ntext\n", wantTitle: "Plain file", wantBody: "# Plain file\ntext\n", wantLine: 2},
		{name: "unclosed", in: "---\ntitle: A\nbody\n", wantErr: true},
		{name: "bad yaml", in: "---\ntitle: [\n---\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := parseNote([]byte(tt.in))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseNote = %+v, want error", n)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n.Title != tt.wantTitle || n.Body != tt.wantBody || n.bodyLine != tt.wantLine {
				t.Errorf("parseNote = title %q body %q line %d, want %q %q %d", n.Title, n.Body, n.bodyLine, tt.wantTitle, tt.wantBody, tt.wantLine)
			}
		})
	}
}

func TestIDAllocation(t *testing.T) {
	tests := []struct {
		name string
		// setup runs after notes 1-3 are created.
		setup  func(t *testing.T, s *Store)
		wantID int
	}{
		{name: "sequential", setup: func(*testing.T, *Store) {}, wantID: 4},
		{
			name: "not reused after trash",
			setup: func(t *testing.T, s *Store) {
				mustTrash(t, s, 3)
			},
			wantID: 4,
		},
		{
			name: "not reused after emptying trash",
			setup: func(t *testing.T, s *Store) {
				mustTrash(t, s, 2, 3)
				if _, err := s.EmptyTrash(); err != nil {
					t.Fatal(err)
				}
			},
			wantID: 4,
		},
		{
			name: "missing counter falls back to highest file",
			setup: func(t *testing.T, s *Store) {
				mustTrash(t, s, 3)
				if err := os.Remove(filepath.Join(s.Dir(), nextIDFile)); err != nil {
					t.Fatal(err)
				}
			},
			wantID: 4,
		},
		{
			name: "file added by hand",
			setup: func(t *testing.T, s *Store) {
				if err := os.WriteFile(filepath.Join(s.Dir(), "0010-manual.md"), []byte("# Manual\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantID: 11,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStore(t)
			created := seedStore(t, s, seed{title: "a"}, seed{title: "b"}, seed{title: "c"})
			if got := ids(created); !slices.Equal(got, []int{1, 2, 3}) {
				t.Fatalf("seed ids = %v", got)
			}
			tt.setup(t, s)
			n, err := s.Create("next", nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if n.ID != tt.wantID {
				t.Errorf("next id = %d, want %d", n.ID, tt.wantID)
			}
		})
	}
}

func mustTrash(t *testing.T, s *Store, ids ...int) {
	t.Helper()
	for _, id := range ids {
		if _, err := s.Trash(id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestList(t *testing.T) {
	s := newStore(t)
	seedStore(t, s,
		seed{title: "Buy groceries", tags: []string{"personal", "errands"}},
		seed{title: "Write report", tags: []string{"work"}, done: true},
		seed{title: "Call mom", tags: []string{"personal"}, done: true},
		seed{title: "Plan trip"},
	)
	tests := []struct {
		name   string
		filter Filter
		want   []int
	}{
		{name: "open", filter: Filter{Status: StatusOpen}, want: []int{1, 4}},
		{name: "done", filter: Filter{Status: StatusDone}, want: []int{2, 3}},
		{name: "all", filter: Filter{Status: StatusAll}, want: []int{1, 2, 3, 4}},
		{name: "tag open", filter: Filter{Status: StatusOpen, Tag: "personal"}, want: []int{1}},
		{name: "tag all ignores case", filter: Filter{Status: StatusAll, Tag: "PERSONAL"}, want: []int{1, 3}},
		{name: "unknown tag", filter: Filter{Status: StatusAll, Tag: "nope"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tt.want) {
				t.Errorf("List = %v, want %v", ids(got), tt.want)
			}
		})
	}
}

func TestDoneUndone(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "a"})
	before, _ := s.Get(1)

	tests := []struct {
		name string
		done bool
	}{
		{name: "done", done: true},
		{name: "done again", done: true},
		{name: "undone", done: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.SetDone(1, tt.done); err != nil {
				t.Fatal(err)
			}
			n, err := s.Get(1)
			if err != nil {
				t.Fatal(err)
			}
			if n.Done != tt.done {
				t.Errorf("Done = %v, want %v", n.Done, tt.done)
			}
			if !n.Updated.After(before.Updated) || !n.Created.Equal(before.Created) {
				t.Errorf("timestamps: created %v updated %v, before %v %v", n.Created, n.Updated, before.Created, before.Updated)
			}
		})
	}

	if _, err := s.SetDone(99, true); !isNotFound(err) {
		t.Errorf("SetDone(99) err = %v, want not found", err)
	}
}

func TestDoing(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "a"})

	n, _ := s.Get(1)
	data, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "doing") {
		t.Errorf("open note's frontmatter mentions doing:\n%s", data)
	}

	steps := []struct {
		name        string
		set         func() (*Note, error)
		done, doing bool
	}{
		{"start", func() (*Note, error) { return s.SetDoing(1, true) }, false, true},
		{"done clears doing", func() (*Note, error) { return s.SetDone(1, true) }, true, false},
		{"start reopens", func() (*Note, error) { return s.SetDoing(1, true) }, false, true},
		{"stop", func() (*Note, error) { return s.SetDoing(1, false) }, false, false},
	}
	for _, st := range steps {
		if _, err := st.set(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		n, err := s.Get(1)
		if err != nil {
			t.Fatal(err)
		}
		if n.Done != st.done || n.Doing != st.doing {
			t.Errorf("%s: done=%v doing=%v, want %v %v", st.name, n.Done, n.Doing, st.done, st.doing)
		}
	}
}

func isNotFound(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

func TestTrashRestore(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "keep"}, seed{title: "toss"})

	n, err := s.Trash(2)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(n.Path) != s.TrashDir() {
		t.Errorf("trashed path = %s, want in %s", n.Path, s.TrashDir())
	}
	if _, err := s.Get(2); !isNotFound(err) {
		t.Errorf("Get trashed note err = %v, want not found", err)
	}
	if all, _ := s.List(Filter{Status: StatusAll}); !slices.Equal(ids(all), []int{1}) {
		t.Errorf("List after trash = %v, want [1]", ids(all))
	}
	trashed, err := s.ListTrash()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(trashed), []int{2}) {
		t.Errorf("ListTrash = %v, want [2]", ids(trashed))
	}

	if _, err := s.Trash(2); !isNotFound(err) {
		t.Errorf("Trash twice err = %v, want not found", err)
	}

	r, err := s.Restore(2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "toss" || filepath.Dir(r.Path) != s.Dir() {
		t.Errorf("Restore = %q at %s", r.Title, r.Path)
	}
	if _, err := s.Get(2); err != nil {
		t.Errorf("Get restored note: %v", err)
	}
	_, err = s.Restore(2)
	if nf, ok := errors.AsType[*NotFoundError](err); !ok || !nf.InTrash {
		t.Errorf("Restore twice err = %v, want not found in trash", err)
	} else if nf.Error() != "note 2 not found in trash" {
		t.Errorf("message = %q", nf.Error())
	}
}

func TestEmptyTrash(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "a"}, seed{title: "b"}, seed{title: "c"})
	mustTrash(t, s, 1, 3)

	n, err := s.EmptyTrash()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("EmptyTrash = %d, want 2", n)
	}
	if left, _ := s.ListTrash(); len(left) != 0 {
		t.Errorf("trash not empty: %v", ids(left))
	}
	if all, _ := s.List(Filter{Status: StatusAll}); !slices.Equal(ids(all), []int{2}) {
		t.Errorf("live notes = %v, want [2]", ids(all))
	}
}

func TestSaveRenamesOnTitleChange(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "Old name"})
	n, _ := s.Get(1)
	oldPath := n.Path

	n.Title = "New name"
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(n.Path) != "0001-new-name.md" {
		t.Errorf("path = %s", n.Path)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file still exists: %v", err)
	}
	if got, _ := s.Get(1); got.Title != "New name" {
		t.Errorf("Get title = %q", got.Title)
	}
}

func TestResolve(t *testing.T) {
	s := newStore(t)
	seedStore(t, s,
		seed{title: "Buy groceries"},
		seed{title: "Buy bike"},
		seed{title: "Fix bike"},
		seed{title: "Fix"},
	)
	tests := []struct {
		ref     string
		want    int
		wantErr string
	}{
		{ref: "1", want: 1},
		{ref: "#3", want: 3},
		{ref: "0002", want: 2},
		{ref: "buy g", want: 1},
		{ref: "FIX B", want: 3},
		{ref: "fix", want: 4}, // exact title beats prefix matches
		{ref: "buy", wantErr: `"buy" matches notes 1, 2; be more specific`},
		{ref: "99", wantErr: "note 99 not found"},
		{ref: "zzz", wantErr: `no note matching "zzz"`},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			n, err := s.Resolve(tt.ref)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Resolve(%q) err = %v, want %q", tt.ref, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n.ID != tt.want {
				t.Errorf("Resolve(%q) = %d, want %d", tt.ref, n.ID, tt.want)
			}
		})
	}
}

func TestSearch(t *testing.T) {
	s := newStore(t)
	seedStore(t, s,
		seed{title: "Buy groceries", tags: []string{"errands"}, body: "- [ ] Milk\n- [ ] eggs\n"},
		seed{title: "Recipes", body: "Pancakes need milk and flour.\n\nAlso MILK for coffee.\n"},
		seed{title: "Milkshake ideas"},
		seed{title: "Taxes", tags: []string{"finance"}},
	)
	tests := []struct {
		q         string
		wantIDs   []int
		wantLines map[int][]int // note ID -> file line numbers
	}{
		{q: "milk", wantIDs: []int{1, 2, 3}, wantLines: map[int][]int{1: {10}, 2: {10, 12}}},
		{q: "ERRANDS", wantIDs: []int{1}},
		{q: "finance", wantIDs: []int{4}},
		{q: "nothing here", wantIDs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			matches, err := s.Search(tt.q)
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, m := range matches {
				got = append(got, m.Note.ID)
				if want, ok := tt.wantLines[m.Note.ID]; ok {
					var lines []int
					for _, l := range m.Lines {
						lines = append(lines, l.N)
						if !strings.Contains(strings.ToLower(l.Text), tt.q) {
							t.Errorf("line %q does not contain %q", l.Text, tt.q)
						}
					}
					if !slices.Equal(lines, want) {
						t.Errorf("note %d lines = %v, want %v", m.Note.ID, lines, want)
					}
				}
			}
			if !slices.Equal(got, tt.wantIDs) {
				t.Errorf("Search(%q) = %v, want %v", tt.q, got, tt.wantIDs)
			}
		})
	}

	if _, err := s.Search("  "); err == nil {
		t.Error("Search with empty query: want error")
	}
}

func TestTags(t *testing.T) {
	s := newStore(t)
	seedStore(t, s,
		seed{title: "a", tags: []string{"work", "urgent"}},
		seed{title: "b", tags: []string{"work"}, done: true},
		seed{title: "c", tags: []string{"home"}},
		seed{title: "d", tags: []string{"work"}},
	)
	mustTrash(t, s, 4)

	got, err := s.Tags()
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"work", 2}, {"home", 1}, {"urgent", 1}}
	if !slices.Equal(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
}

func TestNoTempFilesLeft(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "a"}, seed{title: "b", done: true})
	if _, err := s.Trash(1); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{s.Dir(), s.TrashDir()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tmp-") || e.Name() == lockFile {
				t.Errorf("leftover %s in %s", e.Name(), dir)
			}
		}
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Buy groceries":           "buy-groceries",
		"  --Hello,   World!-- ":  "hello-world",
		"Café déjà vu":            "café-déjà-vu",
		"!!!":                     "note",
		strings.Repeat("ab ", 40): strings.TrimSuffix(strings.Repeat("ab-", 17), "-"),
	}
	for in, want := range tests {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if len(Slug(in)) > maxSlug {
			t.Errorf("Slug(%q) longer than %d", in, maxSlug)
		}
	}
}

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name string
		id   int
		ok   bool
	}{
		{"0007-buy-groceries.md", 7, true},
		{"12.md", 12, true},
		{"0000-zero.md", 0, false},
		{".tmp-123.md", 0, false},
		{"notes.md", 0, false},
		{"0007-buy.txt", 0, false},
		{"+7-x.md", 0, false},
	}
	for _, tt := range tests {
		id, ok := ParseFilename(tt.name)
		if id != tt.id || ok != tt.ok {
			t.Errorf("ParseFilename(%q) = %d, %v, want %d, %v", tt.name, id, ok, tt.id, tt.ok)
		}
	}
}

func TestAppendRetagRename(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "Groceries", tags: []string{"personal"}, body: "- [ ] milk\n\n"})

	n, err := s.Append(1, "- [ ] eggs\n")
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "- [ ] milk\n- [ ] eggs\n" {
		t.Errorf("body after append = %q", n.Body)
	}
	before := n.Updated
	if n, err = s.Append(1, "  \n"); err != nil || !n.Updated.Equal(before) {
		t.Errorf("blank append changed the note: updated %v, err %v", n.Updated, err)
	}

	if n, err = s.Retag(1, []string{"#Work", "home"}, []string{"PERSONAL", "home"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"work", "home"}; !slices.Equal(n.Tags, want) {
		t.Errorf("tags = %v, want %v", n.Tags, want)
	}

	if n, err = s.Rename(1, " Weekly shop "); err != nil {
		t.Fatal(err)
	}
	if n.Title != "Weekly shop" || filepath.Base(n.Path) != "0001-weekly-shop.md" {
		t.Errorf("after rename: title %q, path %s", n.Title, n.Path)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "0001-groceries.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old file still there: %v", err)
	}
	if _, err := s.Rename(1, " "); err == nil {
		t.Error("empty rename succeeded")
	}
	if _, err := s.Append(9, "x"); !isNotFound(err) {
		t.Errorf("append to missing note: %v", err)
	}
}

func TestListOrderAndLimit(t *testing.T) {
	s := newStore(t)
	seedStore(t, s, seed{title: "a"}, seed{title: "b"}, seed{title: "c"})
	if _, err := s.Append(1, "touched last"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		filter Filter
		want   []int
	}{
		{name: "id", filter: Filter{Status: StatusAll}, want: []int{1, 2, 3}},
		{name: "updated", filter: Filter{Status: StatusAll, Order: OrderUpdated}, want: []int{1, 3, 2}},
		{name: "created", filter: Filter{Status: StatusAll, Order: OrderCreated}, want: []int{3, 2, 1}},
		{name: "limit", filter: Filter{Status: StatusAll, Order: OrderUpdated, Limit: 2}, want: []int{1, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tt.want) {
				t.Errorf("List = %v, want %v", ids(got), tt.want)
			}
		})
	}
}

// TestConcurrentWriters checks that the lock keeps read-modify-write
// updates from losing each other and IDs from being handed out twice.
func TestConcurrentWriters(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "notes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("Log", nil, ""); err != nil {
		t.Fatal(err)
	}
	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := range n {
		wg.Go(func() {
			_, err := s.Append(1, fmt.Sprintf("line %d", i))
			errs <- err
		})
		wg.Go(func() {
			_, err := s.Create(fmt.Sprintf("note %d", i), nil, "x")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	log, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(log.Body, "line "); got != n {
		t.Errorf("log has %d appended lines, want %d:\n%s", got, n, log.Body)
	}
	all, err := s.List(Filter{Status: StatusAll})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}; !slices.Equal(ids(all), want) {
		t.Errorf("ids = %v, want %v", ids(all), want)
	}
}

func TestStaleLock(t *testing.T) {
	s := newStore(t)
	lock := filepath.Join(s.Dir(), lockFile)

	// A fresh lock survives an attempt to break it.
	if err := os.WriteFile(lock, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	breakStaleLock(lock)
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("fresh lock was removed: %v", err)
	}

	// An old one is broken, so writes go through.
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("after stale lock", nil, ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.Dir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), lockFile) {
			t.Errorf("left behind %s", e.Name())
		}
	}
}
