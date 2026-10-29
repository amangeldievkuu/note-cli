package editor

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestArgs(t *testing.T) {
	const path = "/notes/0007-buy-groceries.md"
	tests := []struct {
		name      string
		neovim    bool
		editor    string
		env       map[string]string
		onPath    []string
		mode      Mode
		want      []string
		wantWarn  bool
		wantError bool
	}{
		{
			name:   "nvim edit",
			neovim: true,
			onPath: []string{"nvim"},
			mode:   ModeEdit,
			want:   []string{"/bin/nvim", "-c", nvimSettings, "-c", "normal! G$", "--", path},
		},
		{
			name:   "nvim new starts in insert mode",
			neovim: true,
			onPath: []string{"nvim"},
			mode:   ModeNew,
			want:   []string{"/bin/nvim", "-c", nvimSettings, "-c", "normal! G$", "-c", "startinsert!", "--", path},
		},
		{
			name:   "nvim plain",
			neovim: true,
			onPath: []string{"nvim"},
			mode:   ModePlain,
			want:   []string{"/bin/nvim", "--", path},
		},
		{
			name:     "nvim missing falls back with warning",
			neovim:   true,
			env:      map[string]string{"EDITOR": "nano"},
			onPath:   []string{"nano"},
			mode:     ModeNew,
			want:     []string{"/bin/nano", path},
			wantWarn: true,
		},
		{
			name:   "neovim off ignores nvim",
			editor: "",
			env:    map[string]string{"EDITOR": "nano"},
			onPath: []string{"nvim", "nano"},
			mode:   ModeNew,
			want:   []string{"/bin/nano", path},
		},
		{
			name:   "config editor with args wins",
			editor: "code --wait",
			env:    map[string]string{"VISUAL": "hx", "EDITOR": "nano"},
			onPath: []string{"code", "hx", "nano"},
			want:   []string{"/bin/code", "--wait", path},
		},
		{
			name:   "VISUAL before EDITOR",
			env:    map[string]string{"VISUAL": "hx", "EDITOR": "nano"},
			onPath: []string{"hx", "nano"},
			want:   []string{"/bin/hx", path},
		},
		{
			name:   "vi last",
			onPath: []string{"vi"},
			want:   []string{"/bin/vi", path},
		},
		{
			name:      "editor not found",
			editor:    "nope",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warn bytes.Buffer
			l := &Launcher{
				Neovim: tt.neovim,
				Editor: tt.editor,
				LookPath: func(name string) (string, error) {
					if slices.Contains(tt.onPath, name) {
						return "/bin/" + name, nil
					}
					return "", errors.New("not found")
				},
				Getenv: func(k string) string { return tt.env[k] },
				Warn:   &warn,
			}
			got, err := l.Args(path, tt.mode)
			if tt.wantError {
				if err == nil {
					t.Fatalf("Args = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Args =\n %q\nwant\n %q", got, tt.want)
			}
			if gotWarn := strings.Contains(warn.String(), "nvim was not found"); gotWarn != tt.wantWarn {
				t.Errorf("warning = %q, want warning %v", warn.String(), tt.wantWarn)
			}
		})
	}
}

func TestPluginDefinesCommands(t *testing.T) {
	for _, cmd := range []string{"NoteNew", "NoteList", "NoteDone", "NoteOpen"} {
		if !strings.Contains(Plugin(), `nvim_create_user_command("`+cmd+`"`) {
			t.Errorf("plugin does not define :%s", cmd)
		}
	}
}
