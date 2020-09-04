package main

import "strings"

func init() {
	ipReString := "[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}"

	RegisterGameEventHandler("EventConnection",
		"^("+ipReString+"):[0-9]{1,5} is connecting...$",
		handleEventConnection)

	RegisterGameEventHandler("EventPlayerJoin",
		"^Player logged in: (.+), index: ([0-9]+)$",
		handleEventPlayerJoin)

	RegisterGameEventHandler("EventPlayerLeft",
		"^<Server> (.+) left the galaxy$",
		handleEventPlayerLeft)

	RegisterGameEventHandler("EventNone",
		".*",
		defaultEventHandler)
}

func handleEventConnection(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	m := e.Capture.FindStringSubmatch(in)
	go func() { oc <- m[1] }()
}

func handleEventPlayerJoin(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	LogInfo(gs, in, gs.WSOutput())
	m := e.Capture.FindStringSubmatch(in)
	gs.RunCommand("playerinfo -p " + m[1] + " -a -c -t -s")
}

func handleEventPlayerLeft(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	name := strings.TrimPrefix(in, "<Server> ")
	name = strings.TrimSuffix(name, " left the galaxy")

	gs.RemovePlayer(name)
}

func handleEventPlayerBoot(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	m := e.Capture.FindStringSubmatch(in)
	LogInfo(gs, sprintf("Failed connection: %s [%s]", m[1], m[2]), gs.WSOutput())
}

func handleEventPlayerBan(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	LogInfo(gs, in, gs.WSOutput())
}

func handleEventServerPass(gs GameServer, e *GameEvent, in string,
	oc chan string) {
	m := e.Capture.FindStringSubmatch(in)
	gs.SetPassword(m[1])
}
