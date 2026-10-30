// Package cli implements the note command line.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/amangeldievkuu/note-cli/internal/config"
	"github.com/amangeldievkuu/note-cli/internal/editor"
	"github.com/amangeldievkuu/note-cli/internal/store"
)

// app holds what commands share: the loaded config and the store.
type app struct {
	cfgPath string
	cfg     *config.Config
	store   *store.Store
	now     func() time.Time
	debug   bool
}

// load reads the config and opens the store. It is safe to call more than
// once.
func (a *app) load() error {
	if a.store != nil {
		return nil
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	a.cfgPath = path
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%w (config file: %s)", err, path)
	}
	dir, err := cfg.NotesDirPath()
	if err != nil {
		return err
	}
	s, err := store.Open(dir)
	if err != nil {
		return err
	}
	a.cfg, a.store = cfg, s
	return nil
}

func (a *app) launcher(warn io.Writer) *editor.Launcher {
	return &editor.Launcher{Neovim: a.cfg.Neovim, Editor: a.cfg.Editor, Warn: warn}
}

// Execute runs the command line and returns the process exit code.
func Execute(version string) int {
	ctx := context.Background()
	if err := newRootCmd(version).ExecuteContext(ctx); err != nil {
		for line := range strings.Lines(err.Error()) {
			fmt.Fprintln(os.Stderr, "note:", strings.TrimRight(line, "\n"))
		}
		return 1
	}
	return 0
}

func newRootCmd(version string) *cobra.Command {
	a := &app{now: time.Now}

	root := &cobra.Command{
		Use:   "note",
		Short: "Plain-Markdown notes from the command line",
		Long: `note keeps notes as Markdown files with YAML frontmatter.

Run "note" with no command to list open notes.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(cmd.ErrOrStderr(), a.debug || os.Getenv("NOTE_DEBUG") != "")
			return a.load()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runList(cmd, listOptions{})
		},
	}
	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "log debug details to stderr (or set NOTE_DEBUG=1)")
	root.SetVersionTemplate("note {{.Version}}\n")

	root.AddGroup(
		&cobra.Group{ID: "notes", Title: "Notes:"},
		&cobra.Group{ID: "web", Title: "Web view:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)
	for _, c := range []*cobra.Command{
		a.newCmd(), a.listCmd(), a.showCmd(), a.editCmd(),
		a.doneCmd(true), a.doneCmd(false), a.rmCmd(), a.restoreCmd(), a.trashCmd(),
		a.appendCmd(), a.tagCmd(), a.mvCmd(), a.searchCmd(), a.tagsCmd(),
	} {
		c.GroupID = "notes"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.serveCmd(), a.openCmd()} {
		c.GroupID = "web"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.nvimCmd(), a.configCmd()} {
		c.GroupID = "setup"
		root.AddCommand(c)
	}
	root.SetHelpCommandGroupID("setup")
	root.SetCompletionCommandGroupID("setup")
	return root
}

func setupLogging(w io.Writer, debug bool) {
	level := slog.LevelWarn
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.TimeKey && !debug {
				return slog.Attr{}
			}
			return attr
		},
	})))
}

// completeNotes completes note IDs, showing titles as descriptions. With
// trash set it completes trashed notes instead.
func (a *app) completeNotes(trash bool, filter store.Filter) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if err := a.load(); err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var notes []*store.Note
		var err error
		if trash {
			notes, err = a.store.ListTrash()
		} else {
			notes, err = a.store.List(filter)
		}
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var out []cobra.Completion
		for _, n := range notes {
			out = append(out, cobra.CompletionWithDesc(fmt.Sprint(n.ID), n.Title))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeFirst uses complete for the first argument only.
func completeFirst(complete cobra.CompletionFunc) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return complete(cmd, args, toComplete)
	}
}
