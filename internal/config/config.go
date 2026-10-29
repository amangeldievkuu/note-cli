// Package config loads and saves the note configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/amangeldievkuu/note-cli/internal/atomicfile"
)

// Config is the user configuration stored in config.yaml.
type Config struct {
	NotesDir    string `yaml:"notes_dir"`
	Neovim      bool   `yaml:"neovim"`
	Editor      string `yaml:"editor"`
	ServerPort  int    `yaml:"server_port"`
	OpenBrowser bool   `yaml:"open_browser"`
}

// Default returns the configuration used when no file exists.
func Default() *Config {
	return &Config{
		Neovim:      true,
		ServerPort:  7777,
		OpenBrowser: true,
	}
}

// defaultFile is written on first run. Keep it in sync with Default.
const defaultFile = `# note configuration
notes_dir: ""          # empty = default location ($NOTES_DIR, $XDG_DATA_HOME/note, ~/.local/share/note)
neovim: true           # Neovim integration on/off
editor: ""             # fallback editor when neovim is off; empty = $VISUAL/$EDITOR
server_port: 7777
open_browser: true
`

// Path returns the config file location: $XDG_CONFIG_HOME/note/config.yaml,
// falling back to ~/.config/note/config.yaml.
func Path() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "note", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find config dir: %w", err)
	}
	return filepath.Join(home, ".config", "note", "config.yaml"), nil
}

// StateDir returns the directory for runtime state such as the server file:
// $XDG_STATE_HOME/note, falling back to ~/.local/state/note.
func StateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "note"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find state dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "note"), nil
}

// Load reads the config at path. If the file does not exist, it is created
// with the commented defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create config dir: %w", err)
		}
		if err := atomicfile.Write(path, []byte(defaultFile), 0o644); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes config file contents, filling unset keys with defaults.
func Parse(data []byte) (*Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ServerPort <= 0 || cfg.ServerPort > 65535 {
		return nil, fmt.Errorf("parse config: server_port %d out of range", cfg.ServerPort)
	}
	return cfg, nil
}

// NotesDirPath resolves the notes directory: $NOTES_DIR, then notes_dir from
// the config, then $XDG_DATA_HOME/note, then ~/.local/share/note.
func (c *Config) NotesDirPath() (string, error) {
	if dir := os.Getenv("NOTES_DIR"); dir != "" {
		return expandHome(dir)
	}
	if c.NotesDir != "" {
		return expandHome(c.NotesDir)
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "note"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find notes dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "note"), nil
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

// Set changes a single top-level key in the config file at path, keeping
// every other key and all comments. A missing file is created first.
func Set(path, key string, value any) error {
	if _, err := Load(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if doc.Kind == 0 {
		// Empty file: start a fresh mapping.
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config %s: top level is not a mapping", path)
	}

	val, err := scalarNode(value)
	if err != nil {
		return err
	}
	m := doc.Content[0]
	var old *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old = m.Content[i+1]
			break
		}
	}

	// Prefer replacing the value in place so the file's layout, including
	// aligned comments, stays exactly as the user wrote it.
	out, ok := replaceInPlace(data, old, val)
	if !ok {
		if old != nil {
			old.Kind, old.Tag, old.Value, old.Style = val.Kind, val.Tag, val.Value, val.Style
			old.Content = nil
		} else {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&doc); err != nil {
			return fmt.Errorf("encode config: %w", err)
		}
		if err := enc.Close(); err != nil {
			return fmt.Errorf("encode config: %w", err)
		}
		out = buf.Bytes()
	}
	if _, err := Parse(out); err != nil {
		return err
	}
	return atomicfile.Write(path, out, 0o644)
}

// replaceInPlace swaps the text of the plain scalar old for val's value in
// data. It pads or trims the whitespace that follows so a trailing comment
// keeps its column. It reports false when the edit can't be done safely.
func replaceInPlace(data []byte, old, val *yaml.Node) ([]byte, bool) {
	if old == nil || old.Kind != yaml.ScalarNode || old.Style != 0 || val.Style != 0 || old.Line < 1 {
		return nil, false
	}
	lines := strings.SplitAfter(string(data), "\n")
	if old.Line > len(lines) {
		return nil, false
	}
	line := lines[old.Line-1]
	start := old.Column - 1
	end := start + len(old.Value)
	if start < 0 || end > len(line) || line[start:end] != old.Value {
		return nil, false
	}
	rest := line[end:]
	if trimmed := strings.TrimLeft(rest, " "); strings.HasPrefix(trimmed, "#") {
		gap := len(rest) - len(trimmed) + len(old.Value) - len(val.Value)
		rest = strings.Repeat(" ", max(gap, 1)) + trimmed
	}
	lines[old.Line-1] = line[:start] + val.Value + rest
	return []byte(strings.Join(lines, "")), true
}

func scalarNode(v any) (*yaml.Node, error) {
	switch v := v.(type) {
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: yaml.DoubleQuotedStyle}, nil
	default:
		return nil, fmt.Errorf("unsupported config value type %T", v)
	}
}
