# note

A fast, simple note-taking CLI. Notes are plain Markdown files with YAML
frontmatter, so you can grep them, sync them, and edit them with anything.
`note` adds short commands, a rendered terminal view, Neovim integration you
can switch on and off, and a live-reloading web view.

```console
$ note new Buy groceries -t personal,errands -m "- [ ] milk"
Created note 1: Buy groceries
$ note ls
1  [ ]  Buy groceries    #personal #errands  just now
2  [ ]  Draft blog post  #writing            3h ago
$ note done 1
Done: 1 Buy groceries
$ note open        # browse in the browser; pages refresh when you save
```

## Install

With Go 1.27.3 or later:

```sh
go install github.com/amangeldievkuu/note-cli/cmd/note@latest
```

Or from a checkout:

```sh
make install   # static binary (CGO_ENABLED=0) into $(go env GOPATH)/bin
make build     # or just build bin/note
```

`note` runs on Linux and macOS. It also cross-compiles for Windows, but that build is untested.

## Commands

Commands that take an ID also accept a unique, case-insensitive title prefix:
`note done buy` works if only one note title starts with "buy".

| Command | Alias | What it does |
|---|---|---|
| `note new [title...]` | `n`, `add` | Create a note. `-m "text"` or piped stdin saves it right away; otherwise the editor opens, and quitting without saving discards the note. `-t tag1,tag2` adds tags. Prints the new ID. |
| `note list` | `ls` | List open notes: ID, done marker, title, tags, last update. `--all`, `--done`, `-t <tag>`, `--sort id\|updated\|created` (newest first), `-n <limit>`, `--json`. Plain `note` does the same. |
| `note show <id>` | `s` | Render the note in the terminal. `--raw` prints the file as-is; `--json` prints the metadata and body. |
| `note edit <id>` | `e` | Open the note in the editor. `updated` is bumped only if the file changed; changing the title renames the file. |
| `note append <id> [text...]` | | Append text to the note on a new line, from the arguments or piped stdin, without opening the editor. |
| `note tag <id> [+tag\|-tag...]` | | Add (`+tag` or a bare `tag`) and remove (`-tag`) tags. With only an ID, print the note's tags. |
| `note mv <id> <title...>` | | Rename a note (the file is renamed too). |
| `note done <id...>` | `d` | Mark notes done. |
| `note undone <id...>` | | Mark notes not done. |
| `note rm <id...>` | | Move notes to the trash after a y/N prompt. `-f` skips the prompt. |
| `note restore <id>` | | Bring a note back from the trash. |
| `note trash` | | List trashed notes. `--empty` deletes them for good (asks first; `-f` skips). |
| `note search <query>` | `/` | Case-insensitive search across titles, tags, and bodies, with matching lines. `--json` for scripts. |
| `note tags` | | All tags with note counts. |
| `note serve` | | Run the web view in the foreground (see below). |
| `note open [id]` | `o` | Open the web view in the browser, starting a background server if needed. `--print` prints the URL instead. |
| `note nvim [on\|off\|status\|plugin]` | | Toggle Neovim integration (no argument toggles) and save it to the config. `plugin` prints the Lua snippet. |
| `note config [path\|show\|edit]` | | Print the config file path, show the config, or edit it. |
| `note completion bash\|zsh\|fish\|powershell` | | Print a shell completion script. |

Output is colored when stdout is a terminal and plain when piped, and
`NO_COLOR` turns color off. When piped, `note new` prints just the ID:

```sh
id=$(note new "Call the bank" -m "about the card")
echo "- ask about the fee" | note append "$id"
note done "$id"
```

### Shell completion

Completion covers commands, flags, note IDs (with titles), and tags.

```sh
# bash
note completion bash > ~/.local/share/bash-completion/completions/note
# zsh (make sure the directory is in $fpath)
note completion zsh > "${fpath[1]}/_note"
# fish
note completion fish > ~/.config/fish/completions/note.fish
```

## Storage

Notes live in the first of these that is set:

1. `$NOTES_DIR`
2. `notes_dir` in the config
3. `$XDG_DATA_HOME/note`
4. `~/.local/share/note`

The directory is created on first run. Each note is one file,
`<id>-<slug>.md`, for example `0007-buy-groceries.md`:

```markdown
---
id: 7
title: Buy groceries
tags: [personal, errands]
done: false
created: 2026-10-29T10:15:00Z
updated: 2026-10-29T10:15:00Z
---

- [ ] milk
- [ ] eggs
```

- **IDs** are small integers that are never reused, even after the trash is
  emptied. The counter lives in `.nextid`.
- **Deleting** moves the file to `.trash/`. `note restore` moves it back.
- **Writes** go to a temp file that is then renamed into place, so a crash
  never leaves half a note.
- **Hand-made files** work too. Drop a Markdown file named `<number>-anything.md`
  into the directory; frontmatter is optional, and the title falls back to
  the first heading.

## Config

The config file is `$XDG_CONFIG_HOME/note/config.yaml`, falling back to
`~/.config/note/config.yaml`. It is created with these defaults on first run:

```yaml
notes_dir: ""          # empty = default location
neovim: true           # Neovim integration on/off
editor: ""             # fallback editor when neovim is off; empty = $VISUAL/$EDITOR
server_port: 7777
open_browser: true
```

| Key | Meaning |
|---|---|
| `notes_dir` | Where notes live. `~` is expanded. `$NOTES_DIR` overrides it. |
| `neovim` | Open notes in `nvim` with note-friendly settings. See below. |
| `editor` | Editor used when `neovim` is off, arguments allowed (`code --wait`). Empty means `$VISUAL`, then `$EDITOR`, then `vi`. |
| `server_port` | Port for the web view. If it's taken, the next free port is used. |
| `open_browser` | Whether `note serve` opens the browser. Override per run with `--open` or `--no-open`. |

`note nvim on|off` changes only the `neovim` value and leaves the rest of
the file untouched, comments and alignment included.

Other environment variables:

- `NO_COLOR`: disables color.
- `NOTE_DEBUG=1` (or `--debug`): debug logs on stderr.
- `XDG_STATE_HOME`: holds the background server's state and log (default
  `~/.local/state/note`).
- `GLAMOUR_STYLE`: picks the terminal Markdown theme for `note show`
  (`dark`, `light`, `dracula`, …).

## Neovim

With `neovim: true`, `note new` and `note edit` open the file in `nvim` with:

- filetype `markdown`
- `wrap`, `linebreak`, and `spell` set for that buffer only, via `-c
  setlocal …`, so your own config is never changed
- the cursor at the end of the file
- `note new` starting in insert mode on the line below the frontmatter

If `nvim` isn't on your PATH, `note` warns and falls back to the editor
from the config, `$VISUAL`, `$EDITOR`, or `vi`. With `neovim: false` that
editor is used directly, with no Neovim flags.

```sh
note nvim off      # use $EDITOR
note nvim on       # back to Neovim
note nvim          # toggle
note nvim status
```

### Plugin snippet

`note nvim plugin` prints a small Lua file that adds four commands:

- `:NoteNew [title]`
- `:NoteList`, a picker using Telescope or fzf-lua if one is installed,
  otherwise the quickfix list
- `:NoteDone [id]`, which defaults to the note in the current buffer
- `:NoteOpen [id]`, which opens the web view

Each command shells out to the `note` binary. Install it with:

```sh
note nvim plugin > ~/.config/nvim/plugin/note.lua
```

<details>
<summary>The snippet</summary>

```lua
-- note.lua: Neovim commands for the `note` CLI (printed by `note nvim plugin`).
-- Save as ~/.config/nvim/plugin/note.lua, or paste into your init.lua.
--
--   :NoteNew [title]  create a note and start writing
--   :NoteList         pick a note (Telescope, fzf-lua, or the quickfix list)
--   :NoteDone [id]    mark a note done (defaults to the current note)
--   :NoteOpen [id]    open the web view in the browser
--
-- Set vim.g.note_bin if `note` is not on your PATH.
do
  local bin = vim.g.note_bin or "note"

  -- run calls the note binary without a shell and returns its output, or
  -- nil after reporting the error.
  local function run(args)
    local out = vim.fn.system(vim.list_extend({ bin }, args))
    if vim.v.shell_error ~= 0 then
      vim.notify(vim.trim(out), vim.log.levels.ERROR, { title = "note" })
      return nil
    end
    return out
  end

  -- buffer_id returns the note ID of the current buffer from its file name.
  local function buffer_id()
    local name = vim.fn.expand("%:t")
    local id = name:match("^(%d+)%-.*%.md$") or name:match("^(%d+)%.md$")
    return id and tostring(tonumber(id)) or nil
  end

  local function label(n)
    local tags = ""
    if type(n.tags) == "table" and #n.tags > 0 then
      tags = "  #" .. table.concat(n.tags, " #")
    end
    return string.format("%4d %s %s%s", n.id, n.done and "[x]" or "[ ]", n.title, tags)
  end

  local function edit(path)
    vim.cmd.edit(vim.fn.fnameescape(path))
  end

  local function pick(notes)
    if pcall(require, "telescope") then
      local pickers = require("telescope.pickers")
      local finders = require("telescope.finders")
      local conf = require("telescope.config").values
      pickers
        .new({}, {
          prompt_title = "Notes",
          finder = finders.new_table({
            results = notes,
            entry_maker = function(n)
              local l = label(n)
              return { value = n, display = l, ordinal = l, path = n.path, filename = n.path }
            end,
          }),
          sorter = conf.generic_sorter({}),
          previewer = conf.file_previewer({}),
        })
        :find()
      return
    end

    local ok, fzf = pcall(require, "fzf-lua")
    if ok then
      local paths, items = {}, {}
      for _, n in ipairs(notes) do
        local l = label(n)
        paths[l] = n.path
        table.insert(items, l)
      end
      fzf.fzf_exec(items, {
        prompt = "Notes> ",
        actions = {
          ["default"] = function(selected)
            if selected[1] and paths[selected[1]] then
              edit(paths[selected[1]])
            end
          end,
        },
      })
      return
    end

    local items = {}
    for _, n in ipairs(notes) do
      table.insert(items, { filename = n.path, lnum = 1, text = label(n) })
    end
    vim.fn.setqflist({}, " ", { title = "Notes", items = items })
    vim.cmd.copen()
  end

  vim.api.nvim_create_user_command("NoteNew", function(opts)
    local args = { "new", "--print-path" }
    if opts.args ~= "" then
      table.insert(args, opts.args)
    end
    local out = run(args)
    if not out then
      return
    end
    edit(vim.trim(out))
    vim.opt_local.wrap = true
    vim.opt_local.linebreak = true
    vim.opt_local.spell = true
    vim.cmd("normal! G$")
    vim.cmd("startinsert!")
  end, { nargs = "*", desc = "Create a note" })

  vim.api.nvim_create_user_command("NoteList", function()
    local out = run({ "list", "--all", "--json" })
    if not out then
      return
    end
    local ok, notes = pcall(vim.json.decode, out)
    if not ok or type(notes) ~= "table" then
      vim.notify("could not parse `note list --json` output", vim.log.levels.ERROR, { title = "note" })
      return
    end
    if #notes == 0 then
      vim.notify("no notes yet", vim.log.levels.INFO, { title = "note" })
      return
    end
    pick(notes)
  end, { desc = "Pick a note" })

  vim.api.nvim_create_user_command("NoteDone", function(opts)
    local id = opts.args ~= "" and opts.args or buffer_id()
    if not id then
      vim.notify("not a note buffer; pass an id", vim.log.levels.WARN, { title = "note" })
      return
    end
    local current = opts.args == "" or id == buffer_id()
    if current and vim.bo.modified then
      vim.cmd.write()
    end
    local out = run({ "done", id })
    if out then
      vim.notify(vim.trim(out), vim.log.levels.INFO, { title = "note" })
      if current then
        vim.cmd.edit() -- reload the frontmatter change from disk
      end
    end
  end, { nargs = "?", desc = "Mark a note done" })

  vim.api.nvim_create_user_command("NoteOpen", function(opts)
    local cmd = { bin, "open" }
    local id = opts.args ~= "" and opts.args or buffer_id()
    if id then
      table.insert(cmd, id)
    end
    vim.fn.jobstart(cmd, { detach = true })
  end, { nargs = "?", desc = "Open the note web view" })
end
```

</details>

## Web view

```sh
note serve           # foreground; Ctrl-C to stop
note serve --write   # adds "Mark done" and "Delete" buttons
note open            # background server + browser
note open 7          # straight to note 7
note serve --stop    # stop the background server
```

- **Index:** open notes come first and done notes are struck through. You
  can filter by open, done, or all and by tag. The search box filters as
  you type; press `/` to focus it.
- **Note page:** the rendered note with its title, tags, dates, and done
  badge. Markdown support covers GitHub tables, task lists, autolinks,
  footnotes, syntax-highlighted code, and heading anchors. Light and dark
  themes follow your system setting.
- **Live reload:** the notes directory is watched, and open pages refresh
  when a note changes, for example when you `:w` in Neovim.
- **Safety:**
  - The server listens on `127.0.0.1` only.
  - It rejects requests whose `Host` isn't a loopback name, which blocks
    DNS-rebinding attacks.
  - It is read-only unless started with `--write`.
  - Write actions need a per-server CSRF token and must come from the
    same origin.
  - Raw HTML in notes is not rendered.

## Development

```sh
make test    # go test -race ./...
make lint    # gofmt, go vet, staticcheck
make build   # bin/note
make cross   # build check for linux, darwin, windows
```

Layout:

```
cmd/note/            entry point
internal/store/      notes on disk: frontmatter, IDs, trash, search, tags
internal/config/     config file, XDG paths, comment-preserving edits
internal/editor/     Neovim vs. generic editor, the Lua plugin
internal/render/     goldmark (HTML) and glamour (terminal)
internal/server/     HTTP handlers, SSE live reload, background server state
internal/cli/        cobra commands
internal/atomicfile/ temp-file-and-rename writes
web/                 embedded templates, CSS, and JS
```
