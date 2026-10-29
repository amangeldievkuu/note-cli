package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// painter adds ANSI colors when enabled.
type painter struct{ on bool }

// colorFor enables color when w is a terminal, NO_COLOR is unset, and
// TERM isn't "dumb".
func colorFor(w io.Writer) painter {
	f, ok := w.(*os.File)
	return painter{on: ok && term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func (p painter) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p painter) bold(s string) string   { return p.wrap("1", s) }
func (p painter) dim(s string) string    { return p.wrap("2", s) }
func (p painter) strike(s string) string { return p.wrap("2;9", s) }
func (p painter) green(s string) string  { return p.wrap("32", s) }
func (p painter) yellow(s string) string { return p.wrap("33", s) }
func (p painter) cyan(s string) string   { return p.wrap("36", s) }
func (p painter) match(s string) string  { return p.wrap("1;33", s) }

// termWidth returns the width of w if it is a terminal, else fallback.
func termWidth(w io.Writer, fallback int) int {
	if f, ok := w.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			return width
		}
	}
	return fallback
}

// isTerminal reports whether w is a terminal.
func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// cell is one table cell: its text and how to color it.
type cell struct {
	text  string
	style func(string) string
}

// writeTable prints rows with columns padded to the widest visible text.
// Padding is computed before coloring so escape codes don't skew it.
func writeTable(w io.Writer, rows [][]cell) {
	var widths []int
	for _, row := range rows {
		for i, c := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], runewidth.StringWidth(c.text))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i, c := range row {
			text := c.text
			if c.style != nil {
				text = c.style(text)
			}
			b.WriteString(text)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-runewidth.StringWidth(c.text)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// truncate shortens s to at most width display columns.
func truncate(s string, width int) string {
	return runewidth.Truncate(s, width, "…")
}

// confirm asks a yes/no question on errOut and reads the answer from in.
// Anything but "y" or "yes", including end of input, means no.
func confirm(in io.Reader, errOut io.Writer, question string) bool {
	fmt.Fprintf(errOut, "%s [y/N] ", question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
