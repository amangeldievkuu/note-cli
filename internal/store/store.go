// Package store keeps notes as Markdown files with YAML frontmatter in a
// single directory, with a .trash subdirectory for soft-deleted notes.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amangeldievkuu/note/internal/atomicfile"
)

const (
	trashDir   = ".trash"
	nextIDFile = ".nextid"
	lockFile   = ".lock"

	lockStale   = 10 * time.Second
	lockTimeout = 5 * time.Second
)

// Store reads and writes notes in a directory.
type Store struct {
	dir string
	now func() time.Time
}

// Open returns a Store for dir, creating dir and its trash if needed.
func Open(dir string) (*Store, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("notes dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, trashDir), 0o755); err != nil {
		return nil, fmt.Errorf("create notes dir: %w", err)
	}
	return &Store{dir: dir, now: time.Now}, nil
}

// Dir returns the notes directory.
func (s *Store) Dir() string { return s.dir }

// TrashDir returns the directory holding soft-deleted notes.
func (s *Store) TrashDir() string { return filepath.Join(s.dir, trashDir) }

// SetClock replaces the store's time source. Tests use it to get stable
// timestamps.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) timestamp() time.Time {
	return s.now().UTC().Truncate(time.Second)
}

// Create allocates the next ID and writes a new note.
func (s *Store) Create(title string, tags []string, body string) (*Note, error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	id, err := s.allocateID()
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = titleFromBody(body)
	}
	now := s.timestamp()
	n := &Note{ID: id, Title: title, Tags: normalizeTags(tags), Created: now, Updated: now, Body: body}
	if err := s.write(n); err != nil {
		return nil, err
	}
	return n, nil
}

// Get loads the note with the given ID.
func (s *Store) Get(id int) (*Note, error) {
	path, err := findFile(s.dir, id)
	if err != nil {
		return nil, err
	}
	return load(path, id)
}

// PathOf returns the file of the note with the given ID without parsing
// it, so a note with broken frontmatter can still be opened and fixed.
func (s *Store) PathOf(id int) (string, error) {
	return findFile(s.dir, id)
}

// Save writes n back to disk, bumping Updated. If the title changed, the
// file is renamed to match.
func (s *Store) Save(n *Note) error {
	n.Title = strings.TrimSpace(n.Title)
	if n.Title == "" {
		n.Title = titleFromBody(n.Body)
	}
	n.Updated = s.timestamp()
	return s.write(n)
}

func (s *Store) write(n *Note) error {
	data, err := n.marshal()
	if err != nil {
		return fmt.Errorf("note %d: %w", n.ID, err)
	}
	path := filepath.Join(s.dir, Filename(n.ID, n.Title))
	if err := atomicfile.Write(path, data, 0o644); err != nil {
		return fmt.Errorf("save note %d: %w", n.ID, err)
	}
	if n.Path != "" && n.Path != path {
		if err := os.Remove(n.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove old file for note %d: %w", n.ID, err)
		}
	}
	n.Path = path
	return nil
}

// SetDone marks the note done or not done.
func (s *Store) SetDone(id int, done bool) (*Note, error) {
	n, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if n.Done == done {
		return n, nil
	}
	n.Done = done
	return n, s.Save(n)
}

// Trash moves a note into the trash directory.
func (s *Store) Trash(id int) (*Note, error) {
	n, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	dst := filepath.Join(s.TrashDir(), filepath.Base(n.Path))
	if err := os.Rename(n.Path, dst); err != nil {
		return nil, fmt.Errorf("trash note %d: %w", id, err)
	}
	n.Path = dst
	return n, nil
}

// Restore moves a note out of the trash.
func (s *Store) Restore(id int) (*Note, error) {
	src, err := findFile(s.TrashDir(), id)
	if err != nil {
		if nf, ok := errors.AsType[*NotFoundError](err); ok {
			nf.InTrash = true
		}
		return nil, err
	}
	if _, err := findFile(s.dir, id); err == nil {
		return nil, fmt.Errorf("note %d already exists outside the trash", id)
	}
	dst := filepath.Join(s.dir, filepath.Base(src))
	if err := os.Rename(src, dst); err != nil {
		return nil, fmt.Errorf("restore note %d: %w", id, err)
	}
	return load(dst, id)
}

// Discard permanently deletes a note without moving it to the trash. It is
// meant for notes abandoned right after creation.
func (s *Store) Discard(id int) error {
	path, err := findFile(s.dir, id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// EmptyTrash permanently deletes every trashed note and returns how many
// were removed. The ID counter is kept past them so their IDs stay unused.
func (s *Store) EmptyTrash() (int, error) {
	unlock, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()

	if _, err := s.reserveIDs(); err != nil {
		return 0, err
	}
	files, err := noteFiles(s.TrashDir())
	if err != nil {
		return 0, err
	}
	var errs []error
	removed := 0
	for _, f := range files {
		if err := os.Remove(f.path); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// All yields every note outside the trash in ID order. Files that fail to
// parse are logged and skipped; a directory read error is yielded and ends
// the sequence.
func (s *Store) All() iter.Seq2[*Note, error] {
	return notesIn(s.dir)
}

// Trashed yields every note in the trash in ID order.
func (s *Store) Trashed() iter.Seq2[*Note, error] {
	return notesIn(s.TrashDir())
}

func notesIn(dir string) iter.Seq2[*Note, error] {
	return func(yield func(*Note, error) bool) {
		files, err := noteFiles(dir)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, f := range files {
			n, err := load(f.path, f.id)
			if errors.Is(err, fs.ErrNotExist) {
				continue // removed while we were listing
			}
			if err != nil {
				slog.Warn("skipping unreadable note", "path", f.path, "err", err)
				continue
			}
			if !yield(n, nil) {
				return
			}
		}
	}
}

func load(path string, id int) (*Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := parseNote(data)
	if err != nil {
		return nil, fmt.Errorf("note %d (%s): %w", id, filepath.Base(path), err)
	}
	if n.ID != 0 && n.ID != id {
		slog.Warn("frontmatter id does not match file name; using file name", "path", path, "id", n.ID)
	}
	n.ID = id
	n.Path = path
	if n.Created.IsZero() || n.Updated.IsZero() {
		if info, err := os.Stat(path); err == nil {
			mod := info.ModTime().UTC().Truncate(time.Second)
			if n.Created.IsZero() {
				n.Created = mod
			}
			if n.Updated.IsZero() {
				n.Updated = mod
			}
		}
	}
	return n, nil
}

type noteFile struct {
	id   int
	path string
}

// noteFiles lists the note files in dir sorted by ID. Hidden files and
// files without a leading numeric ID are ignored.
func noteFiles(dir string) ([]noteFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read notes dir: %w", err)
	}
	var files []noteFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if id, ok := ParseFilename(e.Name()); ok {
			files = append(files, noteFile{id: id, path: filepath.Join(dir, e.Name())})
		}
	}
	slices.SortFunc(files, func(a, b noteFile) int { return a.id - b.id })
	return files, nil
}

// ParseFilename returns the note ID encoded in a file name such as
// "0007-buy-groceries.md".
func ParseFilename(name string) (int, bool) {
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
		return 0, false
	}
	stem := strings.TrimSuffix(name, ".md")
	digits, _, _ := strings.Cut(stem, "-")
	id, err := strconv.Atoi(digits)
	if err != nil || id <= 0 || strings.HasPrefix(digits, "+") {
		return 0, false
	}
	return id, true
}

func findFile(dir string, id int) (string, error) {
	files, err := noteFiles(dir)
	if err != nil {
		return "", err
	}
	if i, ok := slices.BinarySearchFunc(files, id, func(f noteFile, id int) int { return f.id - id }); ok {
		return files[i].path, nil
	}
	return "", &NotFoundError{Ref: strconv.Itoa(id)}
}

// allocateID returns the next unused ID and advances the counter. The
// caller must hold the lock.
func (s *Store) allocateID() (int, error) {
	id, err := s.reserveIDs()
	if err != nil {
		return 0, err
	}
	if err := s.writeCounter(id + 1); err != nil {
		return 0, err
	}
	return id, nil
}

// reserveIDs makes sure the counter is past every ID in use, including
// trashed notes, and returns its value. The caller must hold the lock.
func (s *Store) reserveIDs() (int, error) {
	next := 1
	if data, err := os.ReadFile(filepath.Join(s.dir, nextIDFile)); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			next = max(next, v)
		}
	}
	stored := next
	for _, dir := range []string{s.dir, s.TrashDir()} {
		files, err := noteFiles(dir)
		if err != nil {
			return 0, err
		}
		if len(files) > 0 {
			next = max(next, files[len(files)-1].id+1)
		}
	}
	if next != stored {
		if err := s.writeCounter(next); err != nil {
			return 0, err
		}
	}
	return next, nil
}

func (s *Store) writeCounter(next int) error {
	if err := atomicfile.Write(filepath.Join(s.dir, nextIDFile), []byte(strconv.Itoa(next)+"\n"), 0o644); err != nil {
		return fmt.Errorf("save id counter: %w", err)
	}
	return nil
}

// lock takes an exclusive lock on the store using a lock file, so two note
// processes never hand out the same ID.
func (s *Store) lock() (unlock func(), err error) {
	path := filepath.Join(s.dir, lockFile)
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("lock notes dir: %w", err)
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > lockStale {
			slog.Debug("removing stale lock", "path", path)
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("notes dir is locked by another process (remove %s if that's not true)", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
