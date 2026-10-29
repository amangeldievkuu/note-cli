package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/amangeldievkuu/note-cli/internal/editor"
	"github.com/amangeldievkuu/note-cli/internal/render"
	"github.com/amangeldievkuu/note-cli/internal/store"
)

func (a *app) newCmd() *cobra.Command {
	var (
		message   string
		tags      []string
		printPath bool
	)
	cmd := &cobra.Command{
		Use:     "new [title...]",
		Aliases: []string{"n", "add"},
		Short:   "Create a note",
		Long: `Create a note and print its ID.

With -m or piped input the note is saved right away. Otherwise the editor
opens; quitting without saving discards the note.`,
		Example: `  note new Buy groceries -t personal,errands
  note n Standup -m "- shipped the parser"
  pbpaste | note add Meeting notes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			body, haveBody := message, cmd.Flags().Changed("message")
			if !haveBody && !printPath && isPiped(cmd.InOrStdin()) {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				body, haveBody = string(data), true
			}

			n, err := a.store.Create(title, tags, body)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if printPath {
				fmt.Fprintln(out, n.Path)
				return nil
			}
			if !haveBody {
				before, err := os.ReadFile(n.Path)
				if err != nil {
					return err
				}
				changed, err := a.runEditor(cmd, n.Path, before, editor.ModeNew)
				if err != nil || !changed {
					if derr := a.store.Discard(n.ID); derr != nil {
						err = errors.Join(err, derr)
					}
					if err == nil {
						fmt.Fprintln(cmd.ErrOrStderr(), "Empty note discarded.")
					}
					return err
				}
				if n, err = a.saveEdited(n.ID, title == ""); err != nil {
					return err
				}
			}
			if isTerminal(out) {
				p := colorFor(out)
				fmt.Fprintf(out, "Created note %s: %s\n", p.cyan(strconv.Itoa(n.ID)), n.Title)
			} else {
				fmt.Fprintln(out, n.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "note body; skips the editor")
	cmd.Flags().StringSliceVarP(&tags, "tags", "t", nil, "comma-separated tags")
	cmd.Flags().BoolVar(&printPath, "print-path", false, "create without an editor and print the file path")
	return cmd
}

// isPiped reports whether r is input piped from another program rather
// than a terminal or /dev/null.
func isPiped(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return r != nil
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice == 0
}

// runEditor opens path and reports whether the file differs from before
// afterwards.
func (a *app) runEditor(cmd *cobra.Command, path string, before []byte, mode editor.Mode) (bool, error) {
	if err := a.launcher(cmd.ErrOrStderr()).Run(path, mode); err != nil {
		return false, err
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}

// saveEdited re-reads a note changed in the editor and saves it, which
// bumps updated and renames the file if the title changed. With
// untitled set, a title still left as "Untitled" is taken from the body.
func (a *app) saveEdited(id int, untitled bool) (*store.Note, error) {
	n, err := a.store.Get(id)
	if err != nil {
		return nil, fmt.Errorf("%w\nyour changes are saved; run \"note edit %d\" to fix the note", err, id)
	}
	if untitled && n.Title == store.Untitled {
		n.Title = "" // Save derives it from the body
	}
	return n, a.store.Save(n)
}

func (a *app) editCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "edit <id>",
		Aliases:           []string{"e"},
		Short:             "Open a note in the editor",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeNotes(false, store.Filter{Status: store.StatusAll}),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id int
			var path string
			if n, err := strconv.Atoi(strings.TrimPrefix(args[0], "#")); err == nil {
				// Look up by ID without parsing so broken notes can be fixed.
				p, err := a.store.PathOf(n)
				if err != nil {
					return err
				}
				id, path = n, p
			} else {
				n, err := a.store.Resolve(args[0])
				if err != nil {
					return err
				}
				id, path = n.ID, n.Path
			}

			before, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			changed, err := a.runEditor(cmd, path, before, editor.ModeEdit)
			if err != nil {
				return err
			}
			if !changed {
				fmt.Fprintln(cmd.ErrOrStderr(), "No changes.")
				return nil
			}
			n, err := a.saveEdited(id, false)
			if err != nil {
				return err
			}
			p := colorFor(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Updated note %s: %s\n", p.cyan(strconv.Itoa(n.ID)), n.Title)
			return nil
		},
	}
}

type listOptions struct {
	all, done bool
	tag       string
	json      bool
}

func (a *app) listCmd() *cobra.Command {
	var o listOptions
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List notes (open ones by default)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runList(cmd, o)
		},
	}
	cmd.Flags().BoolVarP(&o.all, "all", "a", false, "include done notes")
	cmd.Flags().BoolVar(&o.done, "done", false, "only done notes")
	cmd.Flags().StringVarP(&o.tag, "tag", "t", "", "only notes with this tag")
	cmd.Flags().BoolVar(&o.json, "json", false, "print JSON")
	cmd.MarkFlagsMutuallyExclusive("all", "done")
	_ = cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	return cmd
}

func (a *app) completeTags(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if err := a.load(); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	tags, err := a.store.Tags()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var out []cobra.Completion
	for _, t := range tags {
		out = append(out, cobra.CompletionWithDesc(t.Tag, plural(t.Count, "note", "notes")))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// jsonNote is the --json form of a note, used by the Neovim plugin.
type jsonNote struct {
	ID      int       `json:"id"`
	Title   string    `json:"title"`
	Tags    []string  `json:"tags"`
	Done    bool      `json:"done"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Path    string    `json:"path"`
}

func (a *app) runList(cmd *cobra.Command, o listOptions) error {
	f := store.Filter{Status: store.StatusOpen, Tag: o.tag}
	switch {
	case o.all:
		f.Status = store.StatusAll
	case o.done:
		f.Status = store.StatusDone
	}
	notes, err := a.store.List(f)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if o.json {
		items := make([]jsonNote, 0, len(notes))
		for _, n := range notes {
			tags := n.Tags
			if tags == nil {
				tags = []string{}
			}
			items = append(items, jsonNote{n.ID, n.Title, tags, n.Done, n.Created, n.Updated, n.Path})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(notes) == 0 {
		switch {
		case f.Tag != "":
			fmt.Fprintf(out, "No notes tagged %q.\n", f.Tag)
		case f.Status == store.StatusOpen:
			fmt.Fprintln(out, "No open notes. Create one with: note new <title>")
		default:
			fmt.Fprintln(out, "No notes.")
		}
		return nil
	}
	a.printNotes(out, notes)
	return nil
}

// printNotes prints the compact note table.
func (a *app) printNotes(out io.Writer, notes []*store.Note) {
	p := colorFor(out)
	titleWidth := max(20, min(60, termWidth(out, 100)-40))
	now := a.now()
	rows := make([][]cell, 0, len(notes))
	for _, n := range notes {
		mark, markStyle, titleStyle := "[ ]", p.dim, p.bold
		if n.Done {
			mark, markStyle, titleStyle = "[x]", p.green, p.strike
		}
		rows = append(rows, []cell{
			{text: strconv.Itoa(n.ID), style: p.cyan},
			{text: mark, style: markStyle},
			{text: truncate(n.Title, titleWidth), style: titleStyle},
			{text: formatTags(n.Tags), style: p.yellow},
			{text: render.RelTime(n.Updated, now), style: p.dim},
		})
	}
	writeTable(out, rows)
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "#" + strings.Join(tags, " #")
}

func (a *app) showCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:               "show <id>",
		Aliases:           []string{"s"},
		Short:             "Show a note rendered in the terminal",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeNotes(false, store.Filter{Status: store.StatusAll}),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			n, err := a.store.Resolve(args[0])
			if err != nil {
				return err
			}
			if raw {
				data, err := os.ReadFile(n.Path)
				if err != nil {
					return err
				}
				_, err = out.Write(data)
				return err
			}
			p := colorFor(out)
			fmt.Fprint(out, showHeader(p, n, a.now()))
			if strings.TrimSpace(n.Body) == "" {
				fmt.Fprintf(out, "\n  %s\n\n", p.dim("(empty)"))
				return nil
			}
			rendered, err := render.Terminal(n.Body, p.on, min(termWidth(out, 80), 100))
			if err != nil {
				return err
			}
			fmt.Fprint(out, "\n"+strings.TrimLeft(trimLineEnds(rendered), "\n"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print the raw Markdown file")
	return cmd
}

// showHeader is the title and metadata printed above a note's body,
// indented to line up with glamour's margin.
func showHeader(p painter, n *store.Note, now time.Time) string {
	title := p.bold(n.Title)
	if n.Done {
		title = p.bold(p.strike(n.Title))
	}
	meta := []string{p.cyan(fmt.Sprintf("#%d", n.ID))}
	if n.Done {
		meta = append(meta, p.green("✓ done"))
	}
	if len(n.Tags) > 0 {
		meta = append(meta, p.yellow(formatTags(n.Tags)))
	}
	meta = append(meta,
		p.dim("created "+n.Created.Local().Format("2006-01-02 15:04")),
		p.dim("updated "+render.RelTime(n.Updated, now)))
	return fmt.Sprintf("\n  %s\n  %s\n", title, strings.Join(meta, p.dim(" · ")))
}

// trimLineEnds drops the spaces glamour pads each line with.
func trimLineEnds(s string) string {
	var b strings.Builder
	for line := range strings.Lines(s) {
		b.WriteString(strings.TrimRight(line, " \n"))
		if strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (a *app) doneCmd(done bool) *cobra.Command {
	use, short, verb, filter := "done <id...>", "Mark notes done", "Done", store.StatusOpen
	aliases := []string{"d"}
	if !done {
		use, short, verb, filter, aliases = "undone <id...>", "Mark notes not done", "Reopened", store.StatusDone, nil
	}
	return &cobra.Command{
		Use:               use,
		Aliases:           aliases,
		Short:             short,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeNotes(false, store.Filter{Status: filter}),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			p := colorFor(out)
			var errs []error
			for _, ref := range args {
				n, err := a.store.Resolve(ref)
				if err == nil {
					n, err = a.store.SetDone(n.ID, done)
				}
				if err != nil {
					errs = append(errs, err)
					continue
				}
				fmt.Fprintf(out, "%s %s %s\n", p.green(verb+":"), p.cyan(strconv.Itoa(n.ID)), n.Title)
			}
			return errors.Join(errs...)
		},
	}
}

func (a *app) rmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "rm <id...>",
		Short:             "Move notes to the trash",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeNotes(false, store.Filter{Status: store.StatusAll}),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve everything first so a typo doesn't delete half the list.
			var notes []*store.Note
			var errs []error
			for _, ref := range args {
				n, err := a.store.Resolve(ref)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				notes = append(notes, n)
			}
			if err := errors.Join(errs...); err != nil {
				return err
			}

			if !force {
				names := make([]string, len(notes))
				for i, n := range notes {
					names[i] = fmt.Sprintf("%d %q", n.ID, n.Title)
				}
				q := fmt.Sprintf("Move %s to trash (%s)?", plural(len(notes), "note", "notes"), strings.Join(names, ", "))
				if !confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), q) {
					return errors.New("aborted")
				}
			}

			out := cmd.OutOrStdout()
			p := colorFor(out)
			for _, n := range notes {
				if _, err := a.store.Trash(n.ID); err != nil {
					errs = append(errs, err)
					continue
				}
				fmt.Fprintf(out, "%s %s %s\n", p.yellow("Trashed:"), p.cyan(strconv.Itoa(n.ID)), n.Title)
			}
			return errors.Join(errs...)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "don't ask for confirmation")
	return cmd
}

func (a *app) restoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "restore <id>",
		Short:             "Restore a note from the trash",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeNotes(true, store.Filter{}),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := a.store.ResolveTrash(args[0])
			if err != nil {
				return err
			}
			if n, err = a.store.Restore(n.ID); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			p := colorFor(out)
			fmt.Fprintf(out, "%s %s %s\n", p.green("Restored:"), p.cyan(strconv.Itoa(n.ID)), n.Title)
			return nil
		},
	}
}

func (a *app) trashCmd() *cobra.Command {
	var empty, force bool
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List trashed notes, or empty the trash",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			notes, err := a.store.ListTrash()
			if err != nil {
				return err
			}
			if len(notes) == 0 {
				fmt.Fprintln(out, "Trash is empty.")
				return nil
			}
			if !empty {
				a.printNotes(out, notes)
				return nil
			}
			q := fmt.Sprintf("Permanently delete %s in the trash?", plural(len(notes), "note", "notes"))
			if !force && !confirm(cmd.InOrStdin(), cmd.ErrOrStderr(), q) {
				return errors.New("aborted")
			}
			n, err := a.store.EmptyTrash()
			fmt.Fprintf(out, "Deleted %s.\n", plural(n, "note", "notes"))
			return err
		},
	}
	cmd.Flags().BoolVar(&empty, "empty", false, "permanently delete trashed notes")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "don't ask for confirmation with --empty")
	return cmd
}

func (a *app) searchCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "search <query...>",
		Aliases: []string{"/"},
		Short:   "Search titles, tags, and bodies",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := strings.Join(args, " ")
			matches, err := a.store.Search(q)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(matches) == 0 {
				fmt.Fprintf(out, "No matches for %q.\n", q)
				return nil
			}
			p := colorFor(out)
			for i, m := range matches {
				if i > 0 {
					fmt.Fprintln(out)
				}
				title := highlight(p, m.Note.Title, q)
				if m.Note.Done {
					title += " " + p.green("[x]")
				}
				fmt.Fprintf(out, "%s  %s  %s\n", p.cyan(strconv.Itoa(m.Note.ID)), p.bold(title), p.yellow(highlight(p, formatTags(m.Note.Tags), q)))
				for _, l := range m.Lines {
					fmt.Fprintf(out, "  %s %s\n", p.dim(fmt.Sprintf("%4d:", l.N)), highlight(p, strings.TrimSpace(l.Text), q))
				}
			}
			return nil
		},
	}
}

// highlight marks each case-insensitive occurrence of q in s.
func highlight(p painter, s, q string) string {
	lower, lq := strings.ToLower(s), strings.ToLower(q)
	if !p.on || lq == "" || len(lower) != len(s) {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, lq)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(p.match(s[i : i+len(lq)]))
		s, lower = s[i+len(lq):], lower[i+len(lq):]
	}
}

func (a *app) tagsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "List tags with note counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tags, err := a.store.Tags()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(tags) == 0 {
				fmt.Fprintln(out, "No tags yet. Add some with: note new <title> -t tag1,tag2")
				return nil
			}
			p := colorFor(out)
			rows := make([][]cell, len(tags))
			for i, t := range tags {
				rows[i] = []cell{{text: "#" + t.Tag, style: p.yellow}, {text: strconv.Itoa(t.Count), style: p.dim}}
			}
			writeTable(out, rows)
			return nil
		},
	}
}
