package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/amangeldievkuu/note-cli/internal/config"
	"github.com/amangeldievkuu/note-cli/internal/server"
	"github.com/amangeldievkuu/note-cli/internal/store"
)

func statePath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return server.StatePath(dir), nil
}

func (a *app) serveCmd() *cobra.Command {
	var (
		port                int
		write, open, noOpen bool
		stop                bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve notes as a live-reloading web page on 127.0.0.1",
		Long: `Serve notes on 127.0.0.1 (server_port from the config, or the next free
port). Pages reload by themselves when a note file changes.

The web view is read-only unless --write is given, which adds "Mark done",
"Start", and "Delete" buttons.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			sp, err := statePath()
			if err != nil {
				return err
			}
			if stop {
				st, err := server.Stop(cmd.Context(), sp, a.store.Dir())
				if errors.Is(err, server.ErrNotRunning) {
					fmt.Fprintln(out, "No note server is running.")
					return nil
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Stopped the server at %s (pid %d).\n", st.URL, st.PID)
				return nil
			}
			if st, ok := server.Running(cmd.Context(), sp, a.store.Dir()); ok {
				return fmt.Errorf("a note server is already running at %s (pid %d); stop it with \"note serve --stop\"", st.URL, st.PID)
			}

			want := a.cfg.ServerPort
			if cmd.Flags().Changed("port") {
				want = port
			}
			l, err := server.Listen(want)
			if err != nil {
				return err
			}
			srv, err := server.New(a.store, server.Options{Write: write})
			if err != nil {
				return err
			}
			st := server.State{
				PID:      os.Getpid(),
				Port:     l.Addr().(*net.TCPAddr).Port,
				URL:      "http://" + l.Addr().String(),
				NotesDir: a.store.Dir(),
			}
			if err := server.WriteState(sp, st); err != nil {
				return fmt.Errorf("save server state: %w", err)
			}
			defer server.RemoveState(sp, st.PID)

			if st.Port != want {
				fmt.Fprintf(cmd.ErrOrStderr(), "Port %d is taken; using %d.\n", want, st.Port)
			}
			mode := "read-only"
			if write {
				mode = "read-write"
			}
			fmt.Fprintf(out, "Serving %s at %s (%s, Ctrl-C to stop)\n", a.store.Dir(), st.URL, mode)

			openBrowser := a.cfg.OpenBrowser
			if cmd.Flags().Changed("open") {
				openBrowser = open
			}
			if noOpen {
				openBrowser = false
			}
			if openBrowser {
				if err := server.OpenBrowser(st.URL); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "note: warning:", err)
				}
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return srv.Serve(ctx, l)
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 0, "port to listen on (default server_port from the config)")
	cmd.Flags().BoolVar(&write, "write", false, `enable the "Mark done", "Start", and "Delete" buttons`)
	cmd.Flags().BoolVar(&open, "open", false, "open the browser (default open_browser from the config)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	cmd.Flags().BoolVar(&stop, "stop", false, "stop the background server started by \"note open\"")
	cmd.MarkFlagsMutuallyExclusive("open", "no-open")
	return cmd
}

func (a *app) openCmd() *cobra.Command {
	var printURL bool
	cmd := &cobra.Command{
		Use:     "open [id]",
		Aliases: []string{"o"},
		Short:   "Open the web view in the browser, starting the server if needed",
		Long: `Open the web view in the browser, at a note if an ID is given.

If no server is running for the notes directory, one is started in the
background (read-only). Stop it with "note serve --stop".`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeNotes(false, store.Filter{Status: store.StatusAll}),
		RunE: func(cmd *cobra.Command, args []string) error {
			page := "/"
			if len(args) == 1 {
				n, err := a.store.Resolve(args[0])
				if err != nil {
					return err
				}
				page = fmt.Sprintf("/n/%d", n.ID)
			}

			sp, err := statePath()
			if err != nil {
				return err
			}
			st, ok := server.Running(cmd.Context(), sp, a.store.Dir())
			if !ok {
				if st, err = a.startBackground(cmd.Context(), sp); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Started a note server at %s (pid %d).\n", st.URL, st.PID)
			}

			url := st.URL + page
			if printURL {
				fmt.Fprintln(cmd.OutOrStdout(), url)
				return nil
			}
			if err := server.OpenBrowser(url); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Opened %s\n", url)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printURL, "print", false, "print the URL instead of opening a browser")
	return cmd
}

// startBackground runs "note serve --no-open" detached from the terminal
// and waits for it to answer.
func (a *app) startBackground(ctx context.Context, statePath string) (server.State, error) {
	exe, err := os.Executable()
	if err != nil {
		return server.State{}, err
	}
	logPath := filepath.Join(filepath.Dir(statePath), "server.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return server.State{}, err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return server.State{}, err
	}
	defer logf.Close()

	cmd := exec.Command(exe, "serve", "--no-open")
	cmd.Stdout, cmd.Stderr = logf, logf
	server.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return server.State{}, fmt.Errorf("start server: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(5 * time.Second)
	for {
		if st, ok := server.Running(ctx, statePath, a.store.Dir()); ok {
			return st, nil
		}
		select {
		case err := <-exited:
			return server.State{}, fmt.Errorf("server exited during startup (%v); see %s", err, logPath)
		case <-deadline:
			return server.State{}, fmt.Errorf("server did not start within 5s; see %s", logPath)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
