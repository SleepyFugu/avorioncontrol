--[[

  AvorionControl - data/scripts/commands/setdiscorddata.lua
  ---------------------------------------------------------

  This command is for use by the bot, and is used to update the in-game
  data regarding the bots status.

  License: BSD-3-Clause
  https://opensource.org/licenses/BSD-3-Clause

]]

package.path = package.path .. ";data/scripts/lib/?.lua"
include("avocontrol-utils")

function execute(user, cmd, botuser, discordlink)
  if type(user) ~= "nil" then
    return 1, "\\c(f00)Do not run this please.", ""
  end

  local ok = SetConfigData("Discord", {
    discordUrl = discordlink,
    discordBot = botuser})

  if not ok then
    return 1, "Failed to update data", ""
  end

  return 0, "Updated Avorion server Discord values", ""
end

function getDescription()
  return "(Bot only) This updates the Discord information stored on the server"
end

function getHelp()
end