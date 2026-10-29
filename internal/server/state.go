package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/amangeldievkuu/note-cli/internal/atomicfile"
)

// State describes a running server. "note serve" writes it so "note open"
// can find the server and "note serve --stop" can stop it.
type State struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	URL      string `json:"url"`
	NotesDir string `json:"notes_dir"`
}

// StatePath returns the state file inside stateDir.
func StatePath(stateDir string) string {
	return filepath.Join(stateDir, "server.json")
}

// WriteState saves st to path.
func WriteState(path string, st State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'), 0o644)
}

// ReadState loads the state file. A missing file returns fs.ErrNotExist.
func ReadState(path string) (State, error) {
	var st State
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("server state %s: %w", path, err)
	}
	return st, nil
}

// RemoveState deletes the state file if it still belongs to pid, so a
// server shutting down never removes the record of a newer one.
func RemoveState(path string, pid int) {
	if st, err := ReadState(path); err == nil && st.PID == pid {
		os.Remove(path)
	}
}

// Probe reports whether a note server for notesDir answers at url.
func Probe(ctx context.Context, url, notesDir string) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h Health
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&h) != nil {
		return false
	}
	return h.App == "note" && h.NotesDir == notesDir
}

// Running returns the state of a live server for notesDir, if any.
func Running(ctx context.Context, statePath, notesDir string) (State, bool) {
	st, err := ReadState(statePath)
	if err != nil {
		return st, false
	}
	return st, st.URL != "" && Probe(ctx, st.URL, notesDir)
}

// ErrNotRunning is returned by Stop when no server is recorded.
var ErrNotRunning = errors.New("no note server is running")

// Stop signals the server recorded in the state file to shut down and
// waits briefly for it to exit.
func Stop(ctx context.Context, statePath, notesDir string) (State, error) {
	st, ok := Running(ctx, statePath, notesDir)
	if !ok {
		if _, err := os.Stat(statePath); err == nil {
			os.Remove(statePath) // stale
		}
		return st, ErrNotRunning
	}
	p, err := os.FindProcess(st.PID)
	if err != nil {
		return st, err
	}
	if err := terminate(p); err != nil {
		return st, fmt.Errorf("stop server (pid %d): %w", st.PID, err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(statePath); errors.Is(err, fs.ErrNotExist) {
			return st, nil
		}
	}
	return st, nil
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go cmd.Wait() // reap the child; xdg-open can linger
	return nil
}
