--[[

  AvorionControl - data/scripts/lib/avocontrol-command.lua
  --------------------------------------------------------

  Library file for the command mods that AvorionControl utilizes. Adds
  a simple interface for creating a command, and parsing it's arguments.

  Upon sourcing, a valid Avorion command context is created and can be
  executed successfully. This isn't really suitable for small commands,
  but for larger commands with multiple flags/switches it keeps command
  creation quite simple.

  In addition to supporting flags (both short and long), this library
  also provides support for a shell-like -- syntax, which forces all 
  arguments given to a command *after* it is given to be passed directly
  to the commands execute function (as defined by SetExecute).

  License: BSD-3-Clause
  https://opensource.org/licenses/BSD-3-Clause

]]

do
  package.path = package.path .. ";data/scripts/lib/?.lua"
  include("avocontrol-utils")

  local unpack = (type(table.unpack) == "function" and table.unpack or _G.unpack)
  local debug = (FetchConfigData("DEBUGMODE", {debug = "boolean"}).debug
    or false)

  local Command = {
    log         = false,
    name        = "UnsetName",
    flags       = {},
    description = "No description defined",
    execute     = function ()
      return 1, "Command does not have execute set", ""
    end}
  
  Command.__index = Command

  local function validFlag(self, arg)
    if type(arg) ~= "string" then
      return nil, nil
    end

    -- Check the argument against all assigned flags and if one fits, return the
    --  flag index for referencing but not the argument
    for index, flag in ipairs(self.flags) do
      if "-"..flag.short == arg or "--"..flag.long == arg then
        return index, nil
      end
    end

    -- If no matches were found, return the argument but not the index
    return nil, arg
  end

  -- Command.Debug prints the values passed to it when debug mode is
  --  enabled. This method is an empty function when debug mode is
  --  disabled.
  --
  -- Returns:
  --  None
  function Command.Debug(self, ...)
    for _, s in ipairs({...}) do
      print(self:Trace()..tostring(s))
    end
  end

  if not debug then
    Command.Debug = function() end
  end

  -- Command.AddFlag adds an argument definition to the argument definition
  --  list for later processing.
  --
  -- Returns:
  --  @1    Boolean
  function Command.AddFlag(self, d)
    for k, v in pairs({short=d.short, long=d.long, usage=d.usage, help=d.help}) do
      if type(v) ~= "string" then
        print(self:Trace().."AddArgument: Invalid argument for "..k)
        return false, "Script error: Command argument definition is invalid"
      end
    end

    if type(d.func) ~= "function" then
      print(self:Trace().."AddArgument: Invalid function passed (not a function)")
        return false, "Script error: Command argument definition is invalid"
    end

    table.insert(self.flags, {
      execute = d.func,
      passed  = false,
      help    = d.help,
      long    = d.long,
      usage   = d.usage,
      short   = d.short})
    
    -- print("Added flag: "..d.long)
    return true
  end

  -- Command.FlagPassed returns a boolean designating whether or not
  --  a flag was passed to the command. The flag given must be a string
  --  and can refer to either the long or shart version of the flag
  --
  -- Returns:
  --  @1    Boolean
  --  @2    Error
  function Command.FlagPassed(self, flag)
    for _, def in ipairs(self.flags) do
      if def.short == flag or def.long == flag then
        self:Debug("Found: "..flag.."("..tostring(def.passed)..")")
        return def.passed
      end
    end

    return false
  end


  -- Command.ParseFlags parses the arguments provided to the command 
  --  using the functions defined in the Command.flags list via AddFlag
  --
  -- Returns:
  --  @1    Boolean (return status)
  --  @2    Error
  function Command.ParseFlags(self, ...)
    local input = {...}

    if #self.flags < 1 then
      self.data.extra = input
      return true
    end
   
    local cur     = false
    local dump    = false
    local extra   = {}
    local handled = {}

    for _, v in ipairs(input) do
      local flag, arg = validFlag(self, v)

      -- If flag is set, and its data is present, then it's been handled
      --  before and we should specify this.
      if flag then
        self.flags[flag].passed = true
        self:Debug("Flag passed: "..self.flags[flag].long)

        -- If the current argument is a flag, and that flag has already
        --  been handled, run our handler function for that flag and 
        --  flush the data
        if type(self.flags[flag].data) == "table" then
          self:Debug("Handled?",handled[flag], cur.." "..flag)
          if handled[flag] and flag == cur then
            self:Debug("Running flag (extra passed): "..self.flags[flag].long)
            local err = self.flags[cur].execute(unpack(self.flags[cur].data))
            self.flags[cur].data = nil
            if err then
              return false, err
            end
          end
        else
          handled[flag] = false
        end

        cur = flag
        goto continue
      end

      -- If the -- switch is passed, then we stop processing arguments
      --  and dump everything into the extra table. This also clears
      --  said table to prepare for dumping 
      if arg == "--" then
        if cur then
          local err = self.flags[cur].execute(
            unpack(self.flags[cur].data or {}))
          self.flags[cur].data = nil
          if err then
            self:Debug(err)
            return false, err
          end
          cur = false
        end
        extra, dump = {}, true
        goto continue
      end

      if dump then
        table.insert(extra, arg)
        goto continue
      end

      -- Assign any arguments that do not have a given flag to the extra table.
      --  These will be unpacked into the command.execute function
      if not cur and arg then
        self:Debug("Adding argument to extra: "..arg)
        table.insert(extra, arg)
        goto continue
      end

      -- Catch bad inputs. Execution ends here.
      if not cur and not arg then
        return false, "Invalid argument supplied"
      end

      -- Add our argument data and set the to false to complete the input
      if type(self.flags[cur].data) ~= "table" then
        self.flags[cur].data = {}
      end

      table.insert(self.flags[cur].data, arg)      
      handled[cur] = true

      self:Debug("Added ${d} to flag ${f}"%_T % {
        d=arg,
        f=self.flags[cur].long})

      ::continue::
    end

    for i, _ in ipairs(self.flags) do
      if type(self.flags[i].data) == "table" then
        self:Debug("Running flag: "..self.flags[i].long)
        local err = self.flags[i].execute(unpack(self.flags[i].data))
        if type(err) ~= "nil" then
          return false, err
        end
      end
    end

    self.data.extra = extra
    return true
  end


  -- Command.SetExecute sets the primary execution context for the command being
  --  created
  --
  -- Returns:
  --  @1    Boolean
  function Command.SetExecute(self, func)
    if type(func) ~= "function" then
      print(self:Trace().."SetExecute: Bad type (SetExecute expects a function)")
      return false
    end

    self.execute = func
    return true
  end


  -- Command.Execute runs the execution function that we have defined
  --
  -- Returns:
  --  @1    Int (return status)
  --  @2    String Output
  --  @3    String (Avorion uses this for something but its undocumented)
  function Command.Execute(self, user, cmnd, ...)
    local ok, err = self:ParseFlags(...)
    
    if not ok then
      return 1, err, ""
    end
    
    if self.log then
      local u = "rcon"
      if type(user) ~= "nil" then
        u = Player(user).name
      end
      print("${pre}: ${c} was run by ${u}"%_T % {pre="Command", 
        c=self.name, u = u})
    end

    self.data.extra = (type(self.data.extra) == "table" and self.data.extra or {})
    return self.execute(user, unpack(self.data.extra))
  end


  -- Command.GetDescription returns a string containing the commands description
  --
  -- Returns:
  --  @1    String
  function Command.GetDescription(self)
    return self.description
  end


  -- Command.GetHelp generates help text using the set argument definitions and
  --  returns that as a string for output
  --
  -- Return:
  --  @1    String
  function Command.GetHelp(self)
    if #self.flags > 0 then
      local output = "Example: /"..self.name.." [--option|-o] <argument>\n"
        .. (self.description and "  "..self.description.."\n" or "")
        .. (self.help and "  "..self.help.."\n" or "")
        .. "\nOptions:"
      for i, f in ipairs(self.flags) do
        output = "${o}\n  -${s} --${l}\n    ${h}"%_T % {
          o=output,s=f.short,l=f.long,h=f.help}
      end
      return output
    else
      return "Example: /"..self.name
    end
  end


  -- Command.Trace produces tracing information for debug output
  --
  -- Return:
  --  @1    String
  function Command.Trace(self)
    return "avocontrol-command: "..self.name..": "
  end

  local command = setmetatable({data = {}}, Command)

  -- Set the global functions that Avorion looks for. Doing this here means that
  --  simply sourcing our library creates a usable command.
  function _G.getHelp()
    return command:GetHelp()
  end
  
  function _G.getDescription()
    return command:GetDescription()
  end
  
  function _G.execute(...)
    return command:Execute(...)
  end

  return command
end