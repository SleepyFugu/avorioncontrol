--[[

  AvorionControl - data/scripts/entity/avocontrol-shiptracker.lua
  ---------------------------------------------------------------

  Emit ship jump and deletion events to stdout for players and alliances

  License: BSD-3-Clause
  https://opensource.org/licenses/BSD-3-Clause

]]

-- namespace AvorionControlShipTracker
AvorionControlShipTracker = {}
local index = ""

package.path = package.path .. ";data/scripts/lib/?.lua"
include("stringutility")

function AvorionControlShipTracker.initialize()
  if onServer() then
    local ship = Entity()
    local x, y = Sector():getCoordinates()
    index = Uuid(ship.index).number
    print("shipTrackInitEvent: ${oi} ${x}:${y} ${sn}"%_T % {
      oi=ship.factionIndex, x=x, y=y, sn=ship.name})
  end
end

function AvorionControlShipTracker.onSectorChanged()
  if onServer() then
    local ship  = Entity()
    local x, y  = Sector():getCoordinates()
    print("shipJumpEvent: ${oi} ${x}:${y} ${sn}"%_T % {
      oi=ship.factionIndex, x=x, y=y, sn=ship.name})
  end
end