package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sandbox points every note path at a temp dir and returns it.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("NOTES_DIR", filepath.Join(dir, "notes"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	return dir
}

// run executes one note command line with stdin and returns its combined
// output.
func run(t *testing.T, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	cmd.SetIn(stdin)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mustRun(t *testing.T, stdin io.Reader, args ...string) string {
	t.Helper()
	out, err := run(t, stdin, args...)
	if err != nil {
		t.Fatalf("note %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func wantContains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestSession(t *testing.T) {
	dir := sandbox(t)

	if out := mustRun(t, nil, "new", "Buy", "groceries", "-t", "personal,errands", "-m", "- [ ] milk"); out != "1\n" {
		t.Errorf("new printed %q, want the ID", out)
	}
	if out := mustRun(t, strings.NewReader("piped body\n"), "n", "Draft", "post"); out != "2\n" {
		t.Errorf("new from stdin printed %q", out)
	}
	mustRun(t, nil, "add", "Fix bike", "-m", "pump tires")

	out := mustRun(t, nil, "ls")
	wantContains(t, out, "1  [ ]  Buy groceries  #personal #errands", "Draft post", "Fix bike")

	// done accepts several refs and reports every failure.
	out, err := run(t, nil, "done", "1", "99", "nope")
	if err == nil || !strings.Contains(err.Error(), "note 99 not found") || !strings.Contains(err.Error(), `no note matching "nope"`) {
		t.Errorf("done err = %v", err)
	}
	wantContains(t, out, "Done: 1 Buy groceries")

	if out := mustRun(t, nil, "list"); strings.Contains(out, "Buy groceries") {
		t.Errorf("done note in default list:\n%s", out)
	}
	wantContains(t, mustRun(t, nil, "list", "--done"), "[✓]  Buy groceries")
	wantContains(t, mustRun(t, nil, "ls", "--all", "-t", "errands"), "Buy groceries")
	wantContains(t, mustRun(t, nil, "undone", "buy"), "Reopened: 1")

	// start marks a note as doing; starting a done note reopens it, and
	// finishing it clears doing.
	wantContains(t, mustRun(t, nil, "start", "2"), "Started: 2 Draft post")
	wantContains(t, mustRun(t, nil, "ls"), "[●]  Draft post", "[ ]  Fix bike")
	wantContains(t, mustRun(t, nil, "stop", "2"), "Stopped: 2 Draft post")
	wantContains(t, mustRun(t, nil, "ls"), "[ ]  Draft post")
	mustRun(t, nil, "done", "3")
	mustRun(t, nil, "start", "3")
	wantContains(t, mustRun(t, nil, "ls"), "[●]  Fix bike")
	mustRun(t, nil, "done", "3")
	wantContains(t, mustRun(t, nil, "ls", "--done"), "[✓]  Fix bike")
	mustRun(t, nil, "undone", "3")

	// rm asks first; anything but yes aborts.
	if _, err := run(t, strings.NewReader("n\n"), "rm", "2"); err == nil || err.Error() != "aborted" {
		t.Errorf("rm declined err = %v", err)
	}
	wantContains(t, mustRun(t, strings.NewReader("y\n"), "rm", "2"), "Trashed: 2 Draft post")
	wantContains(t, mustRun(t, nil, "trash"), "Draft post")
	wantContains(t, mustRun(t, nil, "restore", "draft"), "Restored: 2 Draft post")
	wantContains(t, mustRun(t, nil, "rm", "-f", "fix"), "Trashed: 3 Fix bike")
	wantContains(t, mustRun(t, strings.NewReader("y\n"), "trash", "--empty"), "Deleted 1 note.")
	if out := mustRun(t, nil, "new", "After empty", "-m", "x"); out != "4\n" {
		t.Errorf("ID after emptying trash = %q, want 4", out)
	}

	wantContains(t, mustRun(t, nil, "/", "MILK"), "1  Buy groceries", "  10: - [ ] milk")
	wantContains(t, mustRun(t, nil, "search", "zzz"), `No matches for "zzz".`)
	wantContains(t, mustRun(t, nil, "tags"), "#errands   1", "#personal  1")

	wantContains(t, mustRun(t, nil, "show", "1", "--raw"), "---\nid: 1\ntitle: Buy groceries\ntags: [personal, errands]\n")
	wantContains(t, mustRun(t, nil, "s", "buy"), "Buy groceries", "#personal #errands", "milk")

	var notes []jsonNote
	if err := json.Unmarshal([]byte(mustRun(t, nil, "list", "--all", "--json")), &notes); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 || notes[0].Path != filepath.Join(dir, "notes", "0001-buy-groceries.md") {
		t.Errorf("json = %+v", notes)
	}

	if _, err := run(t, nil, "show", "42"); err == nil || err.Error() != "note 42 not found" {
		t.Errorf("show missing err = %v", err)
	}
}

func TestNvimToggle(t *testing.T) {
	dir := sandbox(t)
	cfgPath := filepath.Join(dir, "config", "note", "config.yaml")

	if out := mustRun(t, nil, "config", "path"); strings.TrimSpace(out) != cfgPath {
		t.Errorf("config path = %q, want %q", out, cfgPath)
	}
	wantContains(t, mustRun(t, nil, "nvim", "status"), "Neovim integration is on.")
	wantContains(t, mustRun(t, nil, "nvim", "off"), "Neovim integration is off.")
	data, _ := os.ReadFile(cfgPath)
	wantContains(t, string(data), "neovim: false          # Neovim integration on/off", "server_port: 7777")
	wantContains(t, mustRun(t, nil, "nvim"), "Neovim integration is on.")
	wantContains(t, mustRun(t, nil, "nvim"), "Neovim integration is off.")
	wantContains(t, mustRun(t, nil, "nvim", "on"), "Neovim integration is on.")
	wantContains(t, mustRun(t, nil, "nvim", "plugin"), `nvim_create_user_command("NoteList"`)
	if _, err := run(t, nil, "nvim", "maybe"); err == nil {
		t.Error("nvim maybe: want error")
	}

	// A broken config still lets "config show" run.
	if err := os.WriteFile(cfgPath, []byte("neovim: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, nil, "ls"); err == nil {
		t.Error("ls with broken config: want error")
	}
	wantContains(t, mustRun(t, nil, "config", "show"), "warning", "neovim: [")
}

// TestEditorFlow drives "new" and "edit" through a fake editor script.
func TestEditorFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the editor")
	}
	dir := sandbox(t)
	mustRun(t, nil, "nvim", "off")

	script := filepath.Join(dir, "fake-editor")
	setEditor := func(body string) {
		t.Helper()
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("EDITOR", script)
	}
	devNull := func() *os.File {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}

	// Quitting without changes discards the new note.
	setEditor("exit 0\n")
	wantContains(t, mustRun(t, devNull(), "new"), "Empty note discarded.")
	if out := mustRun(t, nil, "ls", "--all"); !strings.Contains(out, "No notes.") {
		t.Errorf("discarded note still listed:\n%s", out)
	}

	// Writing a heading names an untitled note and renames its file.
	setEditor(`printf '# Fresh idea\nbody\n' >> "$1"` + "\n")
	if out := mustRun(t, devNull(), "new"); out != "2\n" {
		t.Errorf("new via editor printed %q", out)
	}
	n, err := os.ReadFile(filepath.Join(dir, "notes", "0002-fresh-idea.md"))
	if err != nil {
		t.Fatal(err)
	}
	wantContains(t, string(n), "title: Fresh idea", "# Fresh idea\nbody\n")

	// edit bumps updated only when the file changes. Backdate it first so
	// a bump is visible even within the same second.
	path := filepath.Join(dir, "notes", "0002-fresh-idea.md")
	const old = "updated: 2020-01-01T00:00:00Z\n"
	backdated := strings.Replace(string(n), updatedLine(string(n)), old, 1)
	if err := os.WriteFile(path, []byte(backdated), 0o644); err != nil {
		t.Fatal(err)
	}
	setEditor("exit 0\n")
	wantContains(t, mustRun(t, nil, "edit", "2"), "No changes.")
	if got, _ := os.ReadFile(path); updatedLine(string(got)) != old {
		t.Errorf("unchanged edit bumped updated: %q", updatedLine(string(got)))
	}
	setEditor(`printf 'more\n' >> "$1"` + "\n")
	wantContains(t, mustRun(t, nil, "e", "fresh"), "Updated note 2: Fresh idea")
	after, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(after), "body\nmore\n") {
		t.Errorf("edit lost content:\n%s", after)
	}
	if got := updatedLine(string(after)); got == old || got == "" {
		t.Errorf("edit did not bump updated: %q", got)
	}

	// A note whose frontmatter was broken can still be opened by ID.
	if err := os.WriteFile(path, []byte("---\ntitle: [\n---\nbroken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setEditor(`printf -- '---\ntitle: Fixed\n---\nok\n' > "$1"` + "\n")
	wantContains(t, mustRun(t, nil, "edit", "2"), "Updated note 2: Fixed")
}

func updatedLine(s string) string {
	for line := range strings.Lines(s) {
		if strings.HasPrefix(line, "updated: ") {
			return line
		}
	}
	return ""
}

func TestQuickEdits(t *testing.T) {
	sandbox(t)
	mustRun(t, nil, "new", "Groceries", "-t", "personal", "-m", "- [ ] milk")

	// Text that looks like a flag is appended as is.
	wantContains(t, mustRun(t, nil, "append", "groc", "- [ ] eggs"), "Appended: 1 Groceries")
	mustRun(t, strings.NewReader("- [ ] bread\n"), "append", "1")
	if _, err := run(t, nil, "append", "1"); err == nil {
		t.Error("append with no text succeeded")
	}

	wantContains(t, mustRun(t, nil, "tag", "1", "+Work", "-personal", "home"), "1 Groceries  #work #home")
	wantContains(t, mustRun(t, nil, "tag", "1"), "#work #home")
	wantContains(t, mustRun(t, nil, "tag", "--help"), "remove them with -tag")

	wantContains(t, mustRun(t, nil, "mv", "1", "Weekly", "shop"), "Renamed: 1 Weekly shop")

	var shown jsonShow
	if err := json.Unmarshal([]byte(mustRun(t, nil, "show", "1", "--json")), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Title != "Weekly shop" || shown.Body != "- [ ] milk\n- [ ] eggs\n- [ ] bread\n" {
		t.Errorf("show --json = %+v", shown)
	}

	var matches []jsonMatch
	if err := json.Unmarshal([]byte(mustRun(t, nil, "search", "eggs", "--json")), &matches); err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || len(matches[0].Lines) != 1 || matches[0].Lines[0].Text != "- [ ] eggs" {
		t.Errorf("search --json = %+v", matches)
	}

	mustRun(t, nil, "new", "Second", "-m", "x")
	mustRun(t, nil, "new", "Third", "-m", "x")
	out := mustRun(t, nil, "ls", "--sort", "created", "-n", "2")
	wantContains(t, out, "Third", "Second")
	if strings.Contains(out, "Weekly") {
		t.Errorf("limit ignored:\n%s", out)
	}
	if _, err := run(t, nil, "ls", "--sort", "bogus"); err == nil {
		t.Error("unknown sort accepted")
	}
}
