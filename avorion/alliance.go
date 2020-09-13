package avorion

import (
	"AvorionControl/ifaces"
	"AvorionControl/logger"
	"fmt"
	"time"
)

// Alliance defines a player alliance in Avorion
type Alliance struct {
	index    string
	name     string
	leader   *Player
	members  []*Player
	loglevel int
	server   *Server

	// alliance data
	resources   map[string]int64
	jumphistory []ifaces.ShipCoordData
}

// Message sends an in-game message to all members of an alliance
func (a *Alliance) Message(string) {
}

// Index returns the faction index of an alliance
func (a *Alliance) Index() string {
	return a.index
}

// Name returns the name of the Alliance
func (a *Alliance) Name() string {
	return a.name
}

// UpdateCoords updates the coordinate DB of the Alliance
func (a *Alliance) UpdateCoords(ifaces.ShipCoordData) {
}

// Update updates the Alliance internal data
func (a *Alliance) Update() error {
	return nil
}

// UpdateFromData updates the alliances information using the data from
//	a successful reAllianceData match
func (a *Alliance) UpdateFromData(d []string) error {
	logger.LogInfo(a, "Updated database")
	return nil
}

// AddJump registers a jump that a player took into a system
func (a *Alliance) AddJump(sc ifaces.ShipCoordData) {
	sc.Time = time.Now()
	a.jumphistory = append(a.jumphistory, sc)
	if len(a.jumphistory) > 1000 {
		a.jumphistory = a.jumphistory[1:]
	}

	logger.LogDebug(a, "Updated jumphistory")
}

/************************/
/* IFace logger.ILogger */
/************************/

// UUID returns the UUID of an alliance
func (a *Alliance) UUID() string {
	return fmt.Sprintf("Alliance:%s:%s", a.index, a.name)
}

// Loglevel returns the loglevel of an alliance
func (a *Alliance) Loglevel() int {
	return a.loglevel
}

// SetLoglevel sets the loglevel of an alliance
func (a *Alliance) SetLoglevel(l int) {
	a.loglevel = l
}
