package store

import (
	"cmp"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
)

// Status selects notes by completion.
type Status int

const (
	StatusOpen Status = iota
	StatusDone
	StatusAll
)

// Order sorts a List result.
type Order int

const (
	OrderID      Order = iota // lowest ID first
	OrderUpdated              // most recently updated first
	OrderCreated              // most recently created first
)

// Filter narrows a List call.
type Filter struct {
	Status Status
	Tag    string // empty matches every note
	Order  Order
	Limit  int // 0 means no limit
}

func (f Filter) match(n *Note) bool {
	switch {
	case f.Status == StatusOpen && n.Done, f.Status == StatusDone && !n.Done:
		return false
	case f.Tag != "" && !n.HasTag(f.Tag):
		return false
	}
	return true
}

// collect gathers the values of seq accepted by keep, stopping at the first
// error.
func collect[T any](seq iter.Seq2[T, error], keep func(T) bool) ([]T, error) {
	var out []T
	for v, err := range seq {
		if err != nil {
			return nil, err
		}
		if keep(v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// List returns the notes matching f in f.Order, at most f.Limit of them.
func (s *Store) List(f Filter) ([]*Note, error) {
	notes, err := collect(s.All(), f.match)
	if err != nil {
		return nil, err
	}
	// Timestamps have one-second resolution, so ties go to the higher,
	// newer ID.
	switch f.Order {
	case OrderUpdated:
		slices.SortFunc(notes, func(a, b *Note) int { return cmp.Or(b.Updated.Compare(a.Updated), b.ID-a.ID) })
	case OrderCreated:
		slices.SortFunc(notes, func(a, b *Note) int { return cmp.Or(b.Created.Compare(a.Created), b.ID-a.ID) })
	}
	if f.Limit > 0 && len(notes) > f.Limit {
		notes = notes[:f.Limit]
	}
	return notes, nil
}

// ListTrash returns the trashed notes in ID order.
func (s *Store) ListTrash() ([]*Note, error) {
	return collect(s.Trashed(), func(*Note) bool { return true })
}

// Resolve finds a note by ID ("7" or "#7") or by a case-insensitive title
// prefix that matches exactly one note. An exact title match wins over
// prefix matches.
func (s *Store) Resolve(ref string) (*Note, error) {
	return resolve(s.All(), ref, s.Get)
}

// ResolveTrash is Resolve for notes in the trash.
func (s *Store) ResolveTrash(ref string) (*Note, error) {
	n, err := resolve(s.Trashed(), ref, func(id int) (*Note, error) {
		path, err := findFile(s.TrashDir(), id)
		if err != nil {
			return nil, err
		}
		return load(path, id)
	})
	if nf, ok := errors.AsType[*NotFoundError](err); ok {
		nf.InTrash = true
	}
	return n, err
}

func resolve(notes iter.Seq2[*Note, error], ref string, byID func(int) (*Note, error)) (*Note, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.Atoi(strings.TrimPrefix(ref, "#")); err == nil {
		if id <= 0 {
			return nil, &NotFoundError{Ref: ref}
		}
		return byID(id)
	}
	if ref == "" {
		return nil, &NotFoundError{Ref: ref}
	}

	lower := strings.ToLower(ref)
	var prefixed []*Note
	for n, err := range notes {
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(n.Title, ref) {
			return n, nil
		}
		if strings.HasPrefix(strings.ToLower(n.Title), lower) {
			prefixed = append(prefixed, n)
		}
	}
	switch len(prefixed) {
	case 0:
		return nil, &NotFoundError{Ref: ref}
	case 1:
		return prefixed[0], nil
	}
	ids := make([]int, len(prefixed))
	for i, n := range prefixed {
		ids[i] = n.ID
	}
	return nil, &AmbiguousError{Ref: ref, IDs: ids}
}

// Line is one line of a note body.
type Line struct {
	N    int // 1-based line number in the file
	Text string
}

// Match is a note that matched a search.
type Match struct {
	Note  *Note
	Title bool   // the title matched
	Tags  bool   // a tag matched
	Lines []Line // body lines that matched
}

// Search finds notes whose title, tags, or body contain q, ignoring case.
func (s *Store) Search(q string) ([]Match, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, fmt.Errorf("empty search query")
	}
	var matches []Match
	for n, err := range s.All() {
		if err != nil {
			return nil, err
		}
		m := Match{
			Note:  n,
			Title: strings.Contains(strings.ToLower(n.Title), q),
			Tags: slices.ContainsFunc(n.Tags, func(t string) bool {
				return strings.Contains(strings.ToLower(t), q)
			}),
		}
		i := 0
		for line := range strings.Lines(n.Body) {
			if strings.Contains(strings.ToLower(line), q) {
				m.Lines = append(m.Lines, Line{N: n.bodyLine + i, Text: strings.TrimRight(line, "\r\n")})
			}
			i++
		}
		if m.Title || m.Tags || len(m.Lines) > 0 {
			matches = append(matches, m)
		}
	}
	return matches, nil
}

// TagCount is a tag and the number of notes carrying it.
type TagCount struct {
	Tag   string
	Count int
}

// Tags counts tags across all notes outside the trash, most used first.
// Tags differing only in case are counted together.
func (s *Store) Tags() ([]TagCount, error) {
	notes, err := collect(s.All(), func(*Note) bool { return true })
	if err != nil {
		return nil, err
	}
	return CountTags(notes), nil
}

// CountTags counts the tags on notes, most used first. Tags differing only
// in case are counted together.
func CountTags(notes []*Note) []TagCount {
	counts := map[string]int{}
	for _, n := range notes {
		for _, t := range normalizeTags(n.Tags) {
			counts[t]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, TagCount{Tag: t, Count: c})
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Tag, b.Tag))
	})
	return out
}
