package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/amangeldievkuu/note/internal/config"
	"github.com/amangeldievkuu/note/internal/editor"
)

func (a *app) nvimCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nvim [on|off|status|plugin]",
		Short: "Turn Neovim integration on or off, or print the Neovim plugin",
		Long: `Turn Neovim integration on or off. With no argument, toggle it.

When on, "note new" and "note edit" open notes in nvim with markdown,
wrap, and spell set for that buffer only. When off, the editor from the
config, $VISUAL, $EDITOR, or vi is used.

"note nvim plugin" prints Lua for :NoteNew, :NoteList, :NoteDone, and
:NoteOpen; save it as ~/.config/nvim/plugin/note.lua.`,
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []cobra.Completion{"on", "off", "status", "plugin"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			action := "toggle"
			if len(args) == 1 {
				action = args[0]
			}
			on := a.cfg.Neovim
			switch action {
			case "plugin":
				fmt.Fprint(out, editor.Plugin())
				return nil
			case "status":
				a.printNvimStatus(cmd)
				return nil
			case "on":
				on = true
			case "off":
				on = false
			case "toggle":
				on = !on
			}
			if err := config.Set(a.cfgPath, "neovim", on); err != nil {
				return err
			}
			a.cfg.Neovim = on
			a.printNvimStatus(cmd)
			return nil
		},
	}
}

func (a *app) printNvimStatus(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	p := colorFor(out)
	if !a.cfg.Neovim {
		fmt.Fprintf(out, "Neovim integration is %s.\n", p.yellow("off"))
		return
	}
	fmt.Fprintf(out, "Neovim integration is %s.\n", p.green("on"))
	if path, ok := a.launcher(nil).NvimFound(); ok {
		fmt.Fprintf(out, "Using %s.\n", path)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: nvim was not found on PATH; note will fall back to $VISUAL/$EDITOR.\n")
	}
}

func (a *app) configCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "config [path|show|edit]",
		Short:     "Show the config file path, print the config, or edit it",
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []cobra.Completion{"path", "show", "edit"},
		// A broken config must not stop "note config edit" from fixing it.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(cmd.ErrOrStderr(), a.debug || os.Getenv("NOTE_DEBUG") != "")
			if err := a.load(); err != nil {
				if a.cfgPath == "" {
					return err
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "note: warning:", err)
				a.cfg = config.Default()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			action := "show"
			if len(args) == 1 {
				action = args[0]
			}
			switch action {
			case "path":
				fmt.Fprintln(out, a.cfgPath)
			case "show":
				data, err := os.ReadFile(a.cfgPath)
				if err != nil {
					return err
				}
				p := colorFor(out)
				fmt.Fprintf(out, "%s\n", p.dim("# "+a.cfgPath))
				out.Write(data)
				if a.store != nil {
					fmt.Fprintf(out, "%s\n", p.dim("# notes are in "+a.store.Dir()))
				}
			case "edit":
				// Open the config like any file: no note-specific settings.
				if err := a.launcher(cmd.ErrOrStderr()).Run(a.cfgPath, editor.ModePlain); err != nil {
					return err
				}
				if _, err := config.Load(a.cfgPath); err != nil {
					return fmt.Errorf("%w\nrun \"note config edit\" to fix it", err)
				}
			}
			return nil
		},
	}
}
