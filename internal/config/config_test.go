package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCreatesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note", "config.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if *cfg != *Default() {
		t.Errorf("Load = %+v, want defaults %+v", *cfg, *Default())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default file not written: %v", err)
	}
	if string(data) != defaultFile {
		t.Errorf("default file = %q, want %q", data, defaultFile)
	}

	// The written defaults must parse back to the same values.
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if *again != *Default() {
		t.Errorf("reloaded = %+v, want %+v", *again, *Default())
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Config
		wantErr bool
	}{
		{
			name: "full",
			in:   "notes_dir: ~/n\nneovim: false\neditor: code -w\nserver_port: 8000\nopen_browser: false\n",
			want: Config{NotesDir: "~/n", Editor: "code -w", ServerPort: 8000},
		},
		{
			name: "partial keeps defaults",
			in:   "neovim: false\n",
			want: Config{ServerPort: 7777, OpenBrowser: true},
		},
		{name: "empty", in: "", want: *Default()},
		{name: "bad yaml", in: "neovim: [", wantErr: true},
		{name: "bad port", in: "server_port: 70000\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.in))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse = %+v, want error", *got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if *got != tt.want {
				t.Errorf("Parse = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	if got, _ := Path(); got != filepath.Join("/xdg/config", "note", "config.yaml") {
		t.Errorf("Path with XDG_CONFIG_HOME = %q", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got, _ := Path(); got != filepath.Join("/home/me", ".config", "note", "config.yaml") {
		t.Errorf("Path fallback = %q", got)
	}
}

func TestNotesDirPath(t *testing.T) {
	tests := []struct {
		name     string
		notesEnv string
		xdgData  string
		cfgDir   string
		want     string
	}{
		{name: "NOTES_DIR wins", notesEnv: "/env/notes", xdgData: "/xdg", cfgDir: "/cfg", want: "/env/notes"},
		{name: "config next", xdgData: "/xdg", cfgDir: "/cfg", want: "/cfg"},
		{name: "config with tilde", cfgDir: "~/my-notes", want: "/home/me/my-notes"},
		{name: "XDG_DATA_HOME", xdgData: "/xdg", want: "/xdg/note"},
		{name: "home fallback", want: "/home/me/.local/share/note"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", "/home/me")
			t.Setenv("NOTES_DIR", tt.notesEnv)
			t.Setenv("XDG_DATA_HOME", tt.xdgData)
			cfg := Default()
			cfg.NotesDir = tt.cfgDir
			got, err := cfg.NotesDirPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != filepath.FromSlash(tt.want) {
				t.Errorf("NotesDirPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetPreservesCommentsAndKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const custom = `# my notes setup
notes_dir: "~/notes"   # synced folder
neovim: true           # Neovim integration on/off
editor: "hx"
server_port: 9000
open_browser: false
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Set(path, "neovim", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{NotesDir: "~/notes", Neovim: false, Editor: "hx", ServerPort: 9000, OpenBrowser: false}
	if *cfg != want {
		t.Errorf("after Set(neovim=false) = %+v, want %+v", *cfg, want)
	}
	data, _ := os.ReadFile(path)
	for _, c := range []string{"# my notes setup", "# synced folder", "# Neovim integration on/off"} {
		if !strings.Contains(string(data), c) {
			t.Errorf("comment %q lost:\n%s", c, data)
		}
	}

	if !strings.Contains(string(data), "neovim: false          # Neovim integration on/off\n") {
		t.Errorf("comment alignment not kept:\n%s", data)
	}

	// Toggling back restores the original file byte for byte.
	if err := Set(path, "neovim", true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != custom {
		t.Errorf("after toggling back:\n%s\nwant:\n%s", data, custom)
	}
}

func TestSetMissingFileAndKey(t *testing.T) {
	dir := t.TempDir()

	// Missing file: created with defaults, then updated.
	path := filepath.Join(dir, "new", "config.yaml")
	if err := Set(path, "neovim", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Neovim || cfg.ServerPort != 7777 {
		t.Errorf("got %+v, want neovim off and default port", *cfg)
	}

	// Missing key: appended.
	path = filepath.Join(dir, "partial.yaml")
	if err := os.WriteFile(path, []byte("server_port: 8080 # custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "neovim", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(path)
	if cfg.Neovim || cfg.ServerPort != 8080 {
		t.Errorf("got %+v, want neovim off and port 8080", *cfg)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# custom") {
		t.Errorf("comment lost:\n%s", data)
	}
}
