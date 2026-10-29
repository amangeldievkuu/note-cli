package store

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Note is a single Markdown note: YAML frontmatter plus a body.
type Note struct {
	ID      int       `yaml:"id"`
	Title   string    `yaml:"title"`
	Tags    []string  `yaml:"tags,flow"`
	Done    bool      `yaml:"done"`
	Created time.Time `yaml:"created"`
	Updated time.Time `yaml:"updated"`

	// Body is the Markdown after the frontmatter.
	Body string `yaml:"-"`
	// Path is the file the note was loaded from or saved to.
	Path string `yaml:"-"`

	// bodyLine is the 1-based file line where Body starts.
	bodyLine int
}

// HasTag reports whether the note carries tag, ignoring case.
func (n *Note) HasTag(tag string) bool {
	return slices.ContainsFunc(n.Tags, func(t string) bool { return strings.EqualFold(t, tag) })
}

const fence = "---"

// marshal renders the note as frontmatter followed by its body.
func (n *Note) marshal() ([]byte, error) {
	meta := *n
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	fm, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	var b bytes.Buffer
	b.WriteString(fence + "\n")
	b.Write(fm)
	b.WriteString(fence + "\n\n")
	if body := strings.Trim(n.Body, "\r\n"); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// parseNote splits data into frontmatter and body. A file without
// frontmatter is treated as all body.
func parseNote(data []byte) (*Note, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	n := &Note{bodyLine: 1}

	if rest, ok := strings.CutPrefix(text, fence+"\n"); ok {
		var meta, body string
		if strings.HasPrefix(rest, fence+"\n") || rest == fence {
			meta, body = "", strings.TrimPrefix(rest, fence)
		} else if i := strings.Index(rest, "\n"+fence+"\n"); i >= 0 {
			meta, body = rest[:i+1], rest[i+len(fence)+1:]
		} else if strings.HasSuffix(rest, "\n"+fence) {
			meta, body = rest[:len(rest)-len(fence)], ""
		} else {
			return nil, fmt.Errorf("frontmatter has no closing %q line", fence)
		}
		if err := yaml.Unmarshal([]byte(meta), n); err != nil {
			return nil, fmt.Errorf("invalid frontmatter: %w", err)
		}
		n.bodyLine = strings.Count(text[:len(text)-len(body)], "\n") + 1
		text = body
	}

	trimmed := strings.TrimLeft(text, "\n")
	n.bodyLine += strings.Count(text[:len(text)-len(trimmed)], "\n")
	n.Body = strings.TrimRight(trimmed, "\n")
	if n.Body != "" {
		n.Body += "\n"
	}
	n.Title = strings.TrimSpace(n.Title)
	if n.Title == "" {
		n.Title = titleFromBody(n.Body)
	}
	return n, nil
}

// Untitled is the title of a note with no title and no body.
const Untitled = "Untitled"

// titleFromBody returns the first non-empty body line without heading
// markers, or Untitled.
func titleFromBody(body string) string {
	for line := range strings.Lines(body) {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line != "" {
			return truncate(line, 80)
		}
	}
	return Untitled
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

const maxSlug = 50

// Slug turns a title into a lowercase, dash-separated file name part.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			dash = true
			continue
		}
		need := utf8.RuneLen(r)
		if dash && b.Len() > 0 {
			need++
		}
		if b.Len()+need > maxSlug {
			break
		}
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		dash = false
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "note"
	}
	return b.String()
}

// Filename returns the file name for a note: "<id>-<slug>.md".
func Filename(id int, title string) string {
	return fmt.Sprintf("%04d-%s.md", id, Slug(title))
}

// normalizeTags trims, lowercases, and de-duplicates tags, keeping order.
func normalizeTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}
