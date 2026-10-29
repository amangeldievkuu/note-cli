// Package editor launches Neovim or the user's editor on a note file.
package editor

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Mode says why the editor is being opened.
type Mode int

const (
	// ModeEdit opens an existing note with the cursor at the end.
	ModeEdit Mode = iota
	// ModeNew opens a fresh note in insert mode below the frontmatter.
	ModeNew
	// ModePlain opens a file with no note-specific settings, e.g. the
	// config file.
	ModePlain
)

// Launcher picks and runs the editor.
type Launcher struct {
	Neovim bool   // use nvim when available
	Editor string // configured fallback editor; may include arguments

	// LookPath and Getenv default to exec.LookPath and os.Getenv.
	LookPath func(string) (string, error)
	Getenv   func(string) string
	// Warn receives warnings such as a missing nvim. Defaults to os.Stderr.
	Warn io.Writer
}

func (l *Launcher) lookPath(name string) (string, error) {
	if l.LookPath != nil {
		return l.LookPath(name)
	}
	return exec.LookPath(name)
}

func (l *Launcher) getenv(key string) string {
	if l.Getenv != nil {
		return l.Getenv(key)
	}
	return os.Getenv(key)
}

func (l *Launcher) warn() io.Writer {
	if l.Warn != nil {
		return l.Warn
	}
	return os.Stderr
}

// nvimSettings apply to the note buffer only, so the user's own config is
// never changed.
const nvimSettings = "setlocal filetype=markdown wrap linebreak spell"

// Args returns the command line used to edit path.
func (l *Launcher) Args(path string, mode Mode) ([]string, error) {
	if l.Neovim {
		nvim, err := l.lookPath("nvim")
		if err == nil {
			args := []string{nvim}
			if mode != ModePlain {
				args = append(args, "-c", nvimSettings, "-c", "normal! G$")
				if mode == ModeNew {
					args = append(args, "-c", "startinsert!")
				}
			}
			return append(args, "--", path), nil
		}
		fmt.Fprintln(l.warn(), "note: warning: neovim is enabled but nvim was not found on PATH; using another editor")
	}

	cmdline := l.Editor
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if cmdline == "" {
			cmdline = l.getenv(key)
		}
	}
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	bin, err := l.lookPath(fields[0])
	if err != nil {
		return nil, fmt.Errorf("editor %q not found: set editor in the config or $VISUAL/$EDITOR", fields[0])
	}
	return append(append([]string{bin}, fields[1:]...), path), nil
}

// Run opens path in the editor and waits for it to exit.
func (l *Launcher) Run(path string, mode Mode) error {
	args, err := l.Args(path, mode)
	if err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", args[0], err)
	}
	return nil
}

// NvimFound reports whether nvim is on PATH and where.
func (l *Launcher) NvimFound() (string, bool) {
	p, err := l.lookPath("nvim")
	return p, err == nil
}

//go:embed plugin.lua
var pluginLua string

// Plugin returns a Lua snippet that adds :NoteNew, :NoteList, :NoteDone
// and :NoteOpen to Neovim.
func Plugin() string { return pluginLua }
