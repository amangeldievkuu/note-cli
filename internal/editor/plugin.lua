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
    return string.format("%4d %s %s%s", n.id, n.done and "[✓]" or n.doing and "[●]" or "[ ]", n.title, tags)
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
