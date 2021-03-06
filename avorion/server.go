package avorion

import (
	gamedb "avorioncontrol/avorion/database"
	"avorioncontrol/avorion/events"
	"avorioncontrol/discord"
	"avorioncontrol/ifaces"
	"avorioncontrol/logger"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

const (
	logUUID            = `AvorionServer`
	errBadIndex        = `invalid index provided (%s)`
	errExecFailed      = `failed to run Avorion binary (%s/bin/%s)`
	errBadDataString   = `failed to parse data string (%s)`
	errEmptyDataString = `got empty data string`
	errFailedRCON      = `failed to run RCON command (%s)`
	errFailToGetData   = `failed to acquire data for %s (%s)`

	warnChatDiscarded = `discarded chat message (time: >5 seconds)`
	warnGameLagging   = `Avorion is lagging, performing restart`

	noticeDBUpate       = `Updating player data DB. Potential lag incoming.`
	regexIntegration    = `^([0-9]+):([0-9]{10})$`
	rconPlayerDiscord   = `linkdiscordacct %s %s`
	rconGetPlayerData   = `getplayerdata -p %s`
	rconGetAllianceData = `getplayerdata -a %s`
	rconGetAllData      = `getplayerdata`
)

var (
	sprintf          = fmt.Sprintf
	regexpDiscordPin = regexp.MustCompile(regexIntegration)

	state    RunState
	cmdmutex *sync.Mutex
)

func init() {
	state = RunState{
		mutex: new(sync.Mutex),
		last:  time.Now()}
	cmdmutex = new(sync.Mutex)
}

// RunState describes the current state of commands being run
type RunState struct {
	mutex *sync.Mutex
	state bool

	//TODO: Migrate all of these into a singular int at
	isrestarting bool
	isstopping   bool
	isstarting   bool
	iscrashed    bool

	// Track the last time the server was started
	last time.Time
}

// Server - Avorion server definition
type Server struct {
	ifaces.IGameServer

	// Execution variables
	Cmd        *exec.Cmd
	executable string
	name       string
	admin      string
	serverpath string
	datapath   string

	// IO
	stdin   io.Writer
	stdout  io.Reader
	output  chan []byte
	chatout chan ifaces.ChatData

	// Logger
	loglevel int
	uuid     string

	//RCON support
	rconpass string
	rconaddr string
	rconport int

	// Game Data
	players   []*Player
	alliances []*Alliance
	sectors   map[int]map[int]*ifaces.Sector
	tracking  *gamedb.TrackingDB

	// Cached values so we don't run loops constantly
	onlineplayers     string
	statusoutput      string
	onlineplayercount int
	playercount       int
	alliancecount     int
	sectorcount       int

	// Config
	configfile string
	config     ifaces.IConfigurator

	// Game information
	password string
	version  string
	seed     string
	motd     string
	time     string

	// Discord
	bot      *discord.Bot
	requests map[string]string

	// Close goroutines
	close chan struct{}
	exit  chan struct{}
	wg    *sync.WaitGroup
}

/********/
/* Main */
/********/

// New returns a new object of type Server
func New(c ifaces.IConfigurator, wg *sync.WaitGroup, exit chan struct{},
	args ...string) ifaces.IGameServer {

	path := c.InstallPath()
	cmnd := "AvorionServer.exe"
	if runtime.GOOS != "windows" {
		cmnd = "AvorionServer"
	}

	version, err := exec.Command(path+"/bin/"+cmnd,
		"--version").Output()
	if err != nil {
		log.Fatal(sprintf(errExecFailed, path, cmnd))
	}

	_, err = exec.Command(c.RCONBin(), "-h").Output()
	if err != nil {
		log.Fatal(sprintf(`Failed to run %s`, c.RCONBin()))
	}

	s := &Server{
		wg:         wg,
		exit:       exit,
		uuid:       logUUID,
		config:     c,
		serverpath: strings.TrimSuffix(path, "/"),
		executable: cmnd,

		version:  string(version),
		rconpass: c.RCONPass(),
		rconaddr: c.RCONAddr(),
		rconport: c.RCONPort(),
		requests: make(map[string]string)}

	s.SetLoglevel(s.config.Loglevel())
	return s
}

// NotifyServer sends an ingame notification
func (s *Server) NotifyServer(in string) error {
	cmd := sprintf("say [NOTIFICATION] %s", in)
	_, err := s.RunCommand(cmd)
	return err
}

/********************************/
/* IFace ifaces.IServer */
/********************************/

// Start starts the Avorion server process
func (s *Server) Start(sendchat bool) error {
	logger.LogDebug(s, "Start() was called")
	state.mutex.Lock()
	state.isstarting = true
	logger.LogDebug(s, "Start() is locking Avorion command state")

	defer func() {
		state.isstarting = false
		state.mutex.Unlock()
		logger.LogDebug(s, "Unlocked Avorion state from Start()")
	}()

	var (
		sectors []*ifaces.Sector
		err     error
	)

	// Catch cases where Avorion is already running
	if s.IsUp() {
		return errors.New("Cannot start server thats already running")
	}

	if s.players != nil {
		s.players = nil
	}

	if s.sectors != nil {
		s.sectors = nil
	}

	// Make sure we are on a fresh server
	s.players = make([]*Player, 0)
	s.sectors = make(map[int]map[int]*ifaces.Sector, 0)
	s.onlineplayercount = 0
	s.statusoutput = ""

	s.InitializeEvents()

	logger.LogInit(s, "Beginning Avorion startup sequence")

	s.name = s.config.Galaxy()
	s.datapath = strings.TrimSuffix(s.config.DataPath(), "/")
	galaxydir := s.datapath + "/" + s.name

	if _, err := os.Stat(galaxydir); os.IsNotExist(err) {
		err := os.Mkdir(galaxydir, 0700)
		if err != nil {
			logger.LogError(s, "os.Mkdir: "+err.Error())
		}
	}

	if err := s.config.BuildModConfig(); err != nil {
		return errors.New("Failed to generate modconfig.lua file")
	}

	s.tracking, err = gamedb.New(sprintf("%s/%s",
		s.config.DataPath(),
		s.config.DBName()))
	if err != nil {
		return err
	}

	sectors, err = s.tracking.Init()
	if err != nil {
		return errors.New("GameDB: " + err.Error())
	}

	s.sectorcount = 0
	for _, sec := range sectors {
		if _, ok := s.sectors[sec.X]; !ok {
			s.sectors[sec.X] = make(map[int]*ifaces.Sector, 0)
		}
		s.sectors[sec.X][sec.Y] = sec
		s.sectorcount++
	}

	s.tracking.SetLoglevel(s.loglevel)

	s.Cmd = exec.Command(
		s.serverpath+"/bin/"+s.executable,
		"--galaxy-name", s.name,
		"--datapath", s.datapath,
		"--admin", s.admin,
		"--rcon-ip", s.config.RCONAddr(),
		"--rcon-password", s.config.RCONPass(),
		"--rcon-port", fmt.Sprint(s.config.RCONPort()))

	s.Cmd.Dir = s.serverpath
	s.Cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH="+s.serverpath+"/linux64")

	if runtime.GOOS != "windows" {
		// This prevents ctrl+c from killing the child process as well as the parent
		// on *Nix systems (not an issue on Windows). Unneeded when running as a unit.
		// https://rosettacode.org/wiki/Check_output_device_is_a_terminal#Go
		if terminal.IsTerminal(int(os.Stdout.Fd())) {
			s.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
		}
	}

	// Doing this prevents errors, but is a stub
	logger.LogDebug(s, "Getting Stdin Pipe")
	if s.stdin, err = s.Cmd.StdinPipe(); err != nil {
		return err
	}

	// Set the STDOUT pipe, so that we can reuse that as needed later
	outr, outw := io.Pipe()
	s.Cmd.Stderr = outw
	s.Cmd.Stdout = outw
	s.stdout = outr

	// Make our intercom channels
	ready := make(chan struct{})  // Avorion is fully up
	s.close = make(chan struct{}) // Close all goroutines

	go superviseAvorionOut(s, ready, s.close)
	go updateAvorionStatus(s, s.close)

	go func() {
		defer func() {
			downstring := strings.TrimSpace(s.config.PostDownCommand())

			if downstring != "" {
				c := make([]string, 0)
				// Split our arguments and add them to the args slice
				for _, m := range regexp.MustCompile(`[^\s]+`).
					FindAllStringSubmatch(downstring, -1) {
					c = append(c, m[0])
				}

				// Only allow the PostDown command to run for 1 minute
				ctx, downcancel := context.WithTimeout(context.Background(), time.Minute)
				defer downcancel()

				// Set the environment
				postdown := exec.CommandContext(ctx, c[0], c[1:]...)
				postdown.Env = append(os.Environ(), "SAVEPATH="+s.datapath+"/"+s.name)

				// Get the output of the PostDown command
				ret, err := postdown.CombinedOutput()
				if err != nil {
					logger.LogError(s, "PostDown: "+err.Error())
				}

				// Log the output
				out := string(ret)
				if out != "" {
					for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
						logger.LogInfo(s, "PostDown: "+line)
					}
				}
			}
		}()

		if err := s.Cmd.Start(); err != nil {
			logger.LogError(s, err.Error())
		}

		logger.LogInit(s, "Started Server and waiting till ready")
		s.Cmd.Wait()
		logger.LogWarning(s, sprintf("Avorion exited with status code (%d)",
			s.Cmd.ProcessState.ExitCode()))
		code := s.Cmd.ProcessState.ExitCode()
		if code != 0 {
			s.Crashed()
			s.SendLog(ifaces.ChatData{Msg: sprintf(
				"**Server Error**: Avorion has exited with non-zero status code: `%d`",
				code)})
		}
		close(s.close)
	}()

	select {
	case <-ready:
		state.iscrashed = false
		logger.LogInit(s, "Server is online")
		s.config.LoadGameConfig()

		// Temporary hack to address a case wherein the playerdata loading occurs too
		// quickly in the games initial startup.
		go func() {
			<-time.After(time.Second * 90)
			s.UpdatePlayerDatabase(false)
		}()

		s.loadSectors()

		// If we have a Post-Up command configured, start that script in a goroutine.
		// We start it there, so that in the event that the script is intende to
		// stay online, it won't block the bot from continuing.
		if upstring := strings.TrimSpace(s.config.PostUpCommand()); upstring != "" {
			go func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				c := make([]string, 0)
				// Split our arguments and add them to the args slice
				for _, m := range regexp.MustCompile(`[^\s]+`).
					FindAllStringSubmatch(upstring, -1) {
					c = append(c, m[0])
				}

				postup := exec.CommandContext(ctx, c[0], c[1:]...)
				postup.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				postup.Env = append(os.Environ(),
					"SAVEPATH="+s.datapath+"/"+s.name,
					"RCONADDR="+s.rconaddr,
					"RCONPASS="+s.rconpass,
					sprintf("RCONPORT=%d", s.rconport))

				// Merge output with AvorionServer. This allows the bot to filter this
				// output along with Avorions without any extra code
				postup.Stdout = outw

				logger.LogInit(s, "Starting PostUp: "+upstring)
				if err := postup.Start(); err != nil {
					logger.LogError(s, "Failed to start configured PostUp command: "+
						upstring)
					logger.LogError(s, "PostUp: "+err.Error())
					postup = nil
					return
				}

				defer func() {
					if postup.ProcessState == nil && postup.Process != nil {
						s.wg.Add(1)
						defer s.wg.Done()
						syscall.Kill(-postup.Process.Pid, syscall.SIGTERM)

						fin := make(chan struct{})
						logger.LogInfo(s, "Waiting for PostUp to stop")

						go func() {
							postup.Wait()
							close(fin)
						}()

						select {
						case <-fin:
							logger.LogInfo(s, "PostUp command stopped")
							return
						case <-time.After(time.Minute):
							logger.LogError(s, "Sending kill to PostUp")
							syscall.Kill(-postup.Process.Pid, syscall.SIGKILL)
							return
						}
					}
				}()

				// Stop the script when we stop the game
				select {
				case <-s.close:
					return
				case <-s.exit:
					return
				}
			}()
		}

		state.last = time.Now()
		return nil

	case <-s.close:
		close(ready)
		state.isstarting = false
		return errors.New("avorion initialization failed")

	case <-time.After(5 * time.Minute):
		close(ready)
		s.Cmd.Process.Kill()
		return errors.New("avorion took over 5 minutes to start")
	}
}

// Stop gracefully stops the Avorion process
func (s *Server) Stop(sendchat bool) error {
	logger.LogDebug(s, "Stop() was called")

	// Lock until any previous state operations are completed
	state.mutex.Lock()
	defer func() {
		state.isstopping = false
		state.mutex.Unlock()
	}()
	state.isstopping = true

	if s.IsUp() != true {
		logger.LogOutput(s, "Server is already offline")
		return nil
	}

	logger.LogInfo(s, "Stopping Avorion server and waiting for it to exit")
	go func() {
		_, err := s.RunCommand("save")
		if err == nil {
			s.RunCommand("stop")
			return
		}
		logger.LogError(s, err.Error())
	}()

	s.onlineplayercount = 0
	stopt := time.After(5 * time.Minute)

	// If the process still exists after 5 minutes have passed kill the server
	// We've SIGKILL'ed the game so it *will* close, so we block until its dead
	// and writes have completed
	select {
	case <-stopt:
		state.iscrashed = true
		s.Cmd.Process.Kill()
		<-s.close
		return errors.New("Avorion took too long to exit and had to be killed")

	// The closer channel will unblock when its closed by Avorions exit, so we can
	// use that to safely detect when this function has completed
	case <-s.close:
		logger.LogInfo(s, "Avorion server has been stopped")
		return nil
	}
}

// Restart restarts the Avorion server
func (s *Server) Restart() error {
	logger.LogDebug(s, "Restart() was called")

	// We don't want to restart if the server was started in the last 10 seconds
	if time.Now().Sub(state.last) > 10 {
		if state.isrestarting || state.isstarting {
			return nil
		}

		if err := s.Stop(false); err != nil {
			logger.LogError(s, err.Error())
		}

		defer func() { state.isrestarting = false }()
		state.isrestarting = true

		if err := s.Start(false); err != nil {
			logger.LogError(s, err.Error())
			return err
		}

		logger.LogInfo(s, "Restarted Avorion")
		return nil
	}

	logger.LogInfo(s, "Server was just started, skipping reboot attempt")
	return errors.New("Server was just restarted")
}

// IsUp checks whether or not the game process is running
func (s *Server) IsUp() bool {
	logger.LogDebug(s, "IsUp() was called")
	if s.Cmd == nil {
		return false
	}

	if s.Cmd.ProcessState != nil {
		return false
	}

	if s.Cmd.Process != nil {
		return true
	}

	return false
}

// Config returns the server configuration struct
func (s *Server) Config() ifaces.IConfigurator {
	return s.config
}

// UpdatePlayerDatabase updates the Avorion player database with all
// of the players that are known to the game
//
// FIXME: Fix this absolute mess of a method
func (s *Server) UpdatePlayerDatabase(notify bool) error {
	logger.LogDebug(s, "UpdatePlayerDatabase() was called")
	var (
		out string
		err error
		m   []string

		allianceCount = 0
		playerCount   = 0
	)

	if notify {
		s.NotifyServer(noticeDBUpate)
	}

	if out, err = s.RunCommand(rconGetAllData); err != nil {
		logger.LogError(s, err.Error())
		return err
	}

	for _, info := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(info, "player: "):
			playerCount++
			if m = rePlayerData.FindStringSubmatch(info); m != nil {
				if p := s.Player(m[1]); p == nil {
					s.NewPlayer(m[1], m)
				}
			} else {
				logger.LogError(s, "player: "+sprintf(errBadDataString, info))
				continue
			}

		case strings.HasPrefix(info, "alliance: "):
			allianceCount++
			if m = reAllianceData.FindStringSubmatch(info); m != nil {
				if a := s.Alliance(m[1]); a == nil {
					s.NewAlliance(m[1], m)
				}
			} else {
				logger.LogError(s, sprintf(errBadDataString, info))
				continue
			}

		case info == "":
			logger.LogWarning(s, "playerdb: "+errEmptyDataString)

		default:
			logger.LogError(s, sprintf(errBadDataString, info))
		}
	}

	s.playercount = playerCount
	s.alliancecount = allianceCount

	for _, p := range s.players {
		s.tracking.SetDiscordToPlayer(p)
		p.SteamUID()
		logger.LogDebug(s, "Processed player: "+p.Name())
	}

	for _, a := range s.alliances {
		logger.LogDebug(s, "Processed alliance: "+a.Name())
	}

	return nil
}

// Status returns a struct containing the current status of the server
func (s *Server) Status() ifaces.ServerStatus {
	logger.LogDebug(s, "Status() was called")

	name := s.name
	if name == "" {
		name = s.config.Galaxy()
	}

	config, _ := s.config.GameConfig()

	return ifaces.ServerStatus{
		Name:          name,
		Status:        s.statusInt(),
		Players:       s.onlineplayers,
		TotalPlayers:  s.playercount,
		PlayersOnline: s.onlineplayercount,
		Alliances:     s.alliancecount,
		Output:        s.statusoutput,
		Sectors:       s.sectorcount,
		INI:           config}
}

// CompareStatus takes two ifaces.ServerStatus arguments and compares
//	them. If they are equivalent, then return true. Else, false.
func (s *Server) CompareStatus(a, b ifaces.ServerStatus) bool {
	logger.LogDebug(s, "CompareStatus() was called")
	if a.Name == b.Name &&
		a.Status == b.Status &&
		a.Players == b.Players &&
		a.TotalPlayers == b.TotalPlayers &&
		a.PlayersOnline == b.PlayersOnline &&
		a.Alliances == b.Alliances &&
		a.Output == b.Output &&
		a.Sectors == b.Sectors {
		return true
	}
	return false
}

// IsCrashed returns the current crash status of the server
func (s *Server) IsCrashed() bool {
	logger.LogDebug(s, "IsCrashed() was called")
	return state.iscrashed
}

// Crashed sets the server status to crashed
func (s *Server) Crashed() {
	logger.LogDebug(s, "Crashed() was called")
	state.iscrashed = true
}

// Recovered sets the server status to be normal (from crashed)
func (s *Server) Recovered() {
	logger.LogDebug(s, "Recovered() was called")
	state.iscrashed = false
}

/************************/
/* IFace logger.ILogger */
/************************/

// UUID returns the UUID of an avorion.Server
func (s *Server) UUID() string {
	return s.uuid
}

// Loglevel returns the loglevel of an avorion.Server
func (s *Server) Loglevel() int {
	return s.loglevel
}

// SetLoglevel sets the loglevel of an avorion.Server
func (s *Server) SetLoglevel(l int) {
	s.loglevel = l
}

/***********************************/
/* IFace ifaces.ICommandableServer */
/***********************************/

// RunCommand runs a command via rcon and returns the output
//	TODO 1: Modify this to use the games rcon websocket interface or an rcon lib
//	TODO 2: Modify this function to make use of permitted command levels
func (s *Server) RunCommand(c string) (string, error) {
	logger.LogDebug(s, sprintf(`RunCommand("%s") was called`, c))

	cmdmutex.Lock()
	logger.LogDebug(s, sprintf("RunCommand(%s) locking", c))

	defer func() {
		cmdmutex.Unlock()
		logger.LogDebug(s, sprintf("Unlocking RunCommand(%s)", c))
	}()

	if s.IsUp() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		// TODO: Make this use an rcon lib
		ret, err := exec.CommandContext(ctx, s.config.RCONBin(), "-H",
			s.rconaddr, "-p", sprintf("%d", s.rconport),
			"-P", s.rconpass, c).CombinedOutput()
		out := string(ret)

		if err != nil {
			logger.LogError(s, "rcon: "+err.Error())
			logger.LogError(s, "rcon: "+out)
			return "", errors.New("Failed to run the following command: " + c)
		}

		if strings.HasPrefix(out, "Unknown command: ") {
			return out, errors.New("Invalid command provided")
		}

		return strings.TrimSuffix(out, "\n"), nil
	}

	return "", errors.New("Server is not online")
}

/*********************************/
/* IFace ifaces.IVersionedServer */
/*********************************/

// SetVersion - Sets the current version of the Avorion server
func (s *Server) SetVersion(v string) {
	s.version = v
}

// Version - Return the version of the Avorion server
func (s *Server) Version() string {
	return s.version
}

/******************************/
/* IFace ifaces.ISeededServer */
/******************************/

// Seed - Return the current game seed
func (s *Server) Seed() string {
	return s.seed
}

// SetSeed - Sets the seed stored in the *Server, *does not* change
// the games seed
func (s *Server) SetSeed(seed string) {
	s.seed = seed
}

/********************************/
/* IFace ifaces.ILockableServer */
/********************************/

// Password - Return the current password
func (s *Server) Password() string {
	return s.password
}

// SetPassword - Set the server password
func (s *Server) SetPassword(p string) {
	s.password = p
}

/****************************/
/* IFace ifaces.IMOTDServer */
/****************************/

// MOTD - Return the current MOTD
func (s *Server) MOTD() string {
	return s.motd
}

// SetMOTD - Set the server MOTD
func (s *Server) SetMOTD(m string) {
	s.motd = m
}

/********************************/
/* IFace ifaces.IPlayableServer */
/********************************/

// Player return a player object that matches the index given
func (s *Server) Player(plrstr string) ifaces.IPlayer {
	// Prefer to check indexes and steamids first as those are faster to check and are more
	// common anyway
	for _, p := range s.players {
		if p.Index() == plrstr {
			return p
		}
	}
	return nil
}

// PlayerFromName return a player object that matches the name given
func (s *Server) PlayerFromName(name string) ifaces.IPlayer {
	for _, p := range s.players {
		logger.LogDebug(s, sprintf("Does (%s) == (%s) ?", p.name, name))
		if p.name == name {
			logger.LogDebug(s, "Found player.")
			return p
		}
	}
	return nil
}

// PlayerFromDiscord return a player object that has been assigned the given
//	Discord user
//
// TODO: Complete this stub
func (s *Server) PlayerFromDiscord(name string) ifaces.IPlayer {
	return nil
}

// Players returns a slice of all of the  players that are known
func (s *Server) Players() []ifaces.IPlayer {
	v := make([]ifaces.IPlayer, 0)
	for _, t := range s.players {
		v = append(v, t)
	}
	return v
}

// Alliance returns a reference to the given alliance
func (s *Server) Alliance(index string) ifaces.IAlliance {
	for _, a := range s.alliances {
		if a.Index() == index {
			return a
		}
	}
	return nil
}

// AllianceFromName returns an alliance object that matches the name given
func (s *Server) AllianceFromName(name string) ifaces.IAlliance {
	for _, a := range s.alliances {
		logger.LogDebug(s, sprintf("Does (%s) == (%s) ?", a.name, name))
		if a.name == name {
			logger.LogDebug(s, "Found player.")
			return a
		}
	}
	return nil
}

// Alliances returns a slice of all of the alliances that are currently known
func (s *Server) Alliances() []ifaces.IAlliance {
	v := make([]ifaces.IAlliance, 0)
	for _, t := range s.alliances {
		v = append(v, t)
	}
	return v
}

// NewPlayer adds a new player to the list of players if it isn't already present
func (s *Server) NewPlayer(index string, d []string) ifaces.IPlayer {
	if _, err := strconv.Atoi(index); err != nil {
		logger.LogError(s, "player: "+sprintf(errBadIndex, index))
		s.Stop(true)
		os.Exit(1)
	}

	cmd := sprintf(rconGetPlayerData, index)

	if len(d) < 15 {
		if data, err := s.RunCommand(cmd); err != nil {
			logger.LogError(s, sprintf(errFailedRCON, err.Error()))
		} else {
			if d = rePlayerData.FindStringSubmatch(data); d == nil {
				logger.LogError(s, sprintf(errBadDataString, data))
				s.Stop(true)
				<-s.close
				panic("Failed to parse data string")
			}
		}
	}

	p := &Player{
		index:       index,
		name:        d[14],
		server:      s,
		jumphistory: make([]ifaces.ShipCoordData, 0),
		loglevel:    s.Loglevel()}

	// Convert our string into an array for safety
	var darr [15]string
	copy(darr[:], d)
	p.UpdateFromData(darr)
	s.players = append(s.players, p)
	if err := s.tracking.TrackPlayer(p); err != nil {
		logger.LogError(s, err.Error())
	}
	logger.LogInfo(p, "Registered player")
	s.playercount++
	return p
}

// RemovePlayer removes a player from the list of online players
// TODO: This function is currently a stub and needs to be made functional once
// more.
func (s *Server) RemovePlayer(n string) {
	return
}

// NewAlliance adds a new alliance to the list of alliances if it isn't already
//	present
func (s *Server) NewAlliance(index string, d []string) ifaces.IAlliance {
	if _, err := strconv.Atoi(index); err != nil {
		logger.LogError(s, "alliance: "+sprintf(errBadIndex, index))
		s.Stop(true)
		os.Exit(1)
	}

	if len(d) < 13 {
		if data, err := s.RunCommand("getplayerdata -a " + index); err != nil {
			logger.LogError(s, sprintf("Failed to get alliance data: (%s)", err.Error()))
		} else {
			if d = rePlayerData.FindStringSubmatch(data); d != nil {
				logger.LogError(s,
					sprintf("alliance: "+errBadDataString, data))
				s.Stop(true)
				<-s.close
				panic("Bad data string given in *Server.NewAlliance")
			}
		}
	}

	if p := s.Alliance(index); p != nil {
		p.Update()
		return p
	}

	a := &Alliance{
		index:       index,
		name:        d[12],
		server:      s,
		jumphistory: make([]ifaces.ShipCoordData, 0),
		loglevel:    s.Loglevel()}

	s.tracking.TrackAlliance(a)
	s.alliances = append(s.alliances, a)
	logger.LogInfo(a, "Registered alliance")
	return a
}

// AddPlayerOnline increments the count of online players
func (s *Server) AddPlayerOnline() {
	s.onlineplayercount++
	s.updateOnlineString()
}

// SubPlayerOnline decrements the count of online players
func (s *Server) SubPlayerOnline() {
	s.onlineplayercount--
	s.updateOnlineString()
}

func (s *Server) updateOnlineString() {
	online := ""
	for _, p := range s.players {
		if p.Online() {
			online = sprintf("%s\n%s", online, p.Name())
		}
	}
	s.onlineplayers = online
	logger.LogDebug(s, "Updated online string: "+s.onlineplayers)
}

/*****************************************/
/* IFace ifaces.IDiscordIntegratedServer */
/*****************************************/

// AddIntegrationRequest registers a request by a player for Discord integration
// TODO: Move this to our sqlite DB
func (s *Server) AddIntegrationRequest(index, pin string) {
	s.requests[index] = pin
}

// ValidateIntegrationPin confirms that a given pin was indeed a valid request
//	and registers the integration
func (s *Server) ValidateIntegrationPin(in, discordID string) bool {
	m := regexpDiscordPin.FindStringSubmatch(in)
	if len(m) < 2 {
		logger.LogError(s, sprintf("Invalid integration request provided: [%s]/[%s]",
			in, discordID))
		return false
	}

	if val, ok := s.requests[m[1]]; ok {
		if val == m[2] {
			s.tracking.AddIntegration(discordID, s.Player(m[1]))
			s.addIntegration(m[1], discordID)
			return true
		}
	}

	return false
}

/******************************/
/* IFace ifaces.IGalaxyServer */
/******************************/

// Sector returns a pointer to a sector object (new or prexisting)
func (s *Server) Sector(x, y int) *ifaces.Sector {
	// Make sure we have an X
	if _, ok := s.sectors[x]; !ok {
		s.sectors[x] = make(map[int]*ifaces.Sector, 0)
	}

	if _, ok := s.sectors[x][y]; !ok {
		s.sectors[x][y] = &ifaces.Sector{
			X: x, Y: y, Jumphistory: make([]*ifaces.JumpInfo, 0)}
		logger.LogInfo(s, sprintf("Tracking new sector: (%d:%d)", x, y))

		// TODO: This performs unnecessarily expensive DB calls here. Granted,
		// that ONLY affects initilization, but it should still be optimized
		s.tracking.TrackSector(s.sectors[x][y])
		s.sectorcount++
	}

	return s.sectors[x][y]
}

// SendChat sends an ifaces.ChatData object to the discord bot if chatting is
//	currently enabled in the configuration
func (s *Server) SendChat(input ifaces.ChatData) {
	if s.config.ChatPipe() != nil {
		if len(input.Msg) >= 2000 {
			logger.LogInfo(s, "Truncated player message for sending")
			input.Msg = input.Msg[0:1900]
			input.Msg += "...(truncated)"
		}

		select {
		case s.Config().ChatPipe() <- input:
			logger.LogDebug(s, "Sent chat data to bot")
		case <-time.After(time.Second * 5):
			logger.LogWarning(s, warnChatDiscarded)
		}
	}
}

// SendLog sends an ifaces.ChatData object to the discord bot if logging is
//	currently enabled in the configuration
func (s *Server) SendLog(input ifaces.ChatData) {
	if s.config.LogPipe() != nil {
		if len(input.Msg) >= 2000 {
			logger.LogInfo(s, "Truncated log for sending")
			input.Msg = input.Msg[0:1900]
			input.Msg += "...(truncated)"
		}

		select {
		case s.Config().LogPipe() <- input:
			logger.LogDebug(s, "Sent event log to bot")
		case <-time.After(time.Second * 5):
			logger.LogWarning(s, warnChatDiscarded)
		}
	}
}

// addIntegration is a helper function that registers an integration
func (s *Server) addIntegration(index, discordID string) {
	s.RunCommand(sprintf(rconPlayerDiscord, index, discordID))
}

// InitializeEvents runs the event initializer
func (s *Server) InitializeEvents() {
	// Re-init our events and apply custom logged events
	events.Initialize()

	var (
		regexPlayerIndex   = regexp.MustCompile(`^player:([0-9]+)$`)
		regexAllianceIndex = regexp.MustCompile(`^alliance:([0-9]+)$`)
	)

	for _, ed := range s.config.GetEvents() {
		ge := &events.Event{
			FString: ed.FString,
			Capture: ed.Regex,
			Handler: func(srv ifaces.IGameServer, e *events.Event,
				in string, oc chan string) {

				logger.LogOutput(s, in)
				logger.LogDebug(e, "Got event: "+e.FString)
				m := e.Capture.FindStringSubmatch(in)
				strings := make([]interface{}, 0)

				// Attempt to match against our player/alliance database and set that
				// string to be the name of said object
				for _, v := range m {
					switch {
					case regexPlayerIndex.MatchString(v):
						v = regexPlayerIndex.FindStringSubmatch(v)[1]
						p := s.Player(v)
						if p != nil {
							v = p.Name()
						}

					case regexAllianceIndex.MatchString(v):
						v = regexAllianceIndex.FindStringSubmatch(v)[1]
						a := s.Alliance(v)
						if a != nil {
							v = a.Name()
						}
					}

					strings = append(strings, v)
				}

				srv.SendLog(ifaces.ChatData{
					Msg: sprintf(e.FString, strings[1:]...)})
			}}

		ge.SetLoglevel(s.Loglevel())

		if err := events.Add(ed.Name, ge); err != nil {
			logger.LogWarning(s, "Failed to register event: "+err.Error())
			continue
		}
	}

	// Handle unmanaged text. We initilialize this last so that all other events
	// are handled first.
	events.New("EventNone", ".*", func(srv ifaces.IGameServer, e *events.Event,
		in string, oc chan string) {
		logger.LogOutput(srv, in)
	})

	logger.LogInit(s, "Completed event registration")
}

// TODO: Make this less godawful
func (s *Server) loadSectors() {
	for _, x := range s.sectors {
		for _, sec := range x {
			for _, j := range sec.Jumphistory {
				for _, p := range s.players {
					if p.Index() == strconv.FormatInt(int64(j.FID), 10) {
						p.jumphistory = append(p.jumphistory, ifaces.ShipCoordData{
							X: j.X, Y: j.Y, Name: j.Name, Time: j.Time})
					}
				}

				for _, a := range s.alliances {
					if a.Index() == strconv.FormatInt(int64(j.FID), 10) {
						a.jumphistory = append(a.jumphistory, ifaces.ShipCoordData{
							X: j.X, Y: j.Y, Name: j.Name, Time: j.Time})
					}
				}
			}
		}
	}

	for _, p := range s.players {
		sort.Sort(jumpsByTime(p.jumphistory))
	}

	for _, a := range s.alliances {
		sort.Sort(jumpsByTime(a.jumphistory))
	}
}

func (s *Server) statusInt() int {
	var sint = ifaces.ServerOffline

	switch {
	case state.isrestarting:
		sint = ifaces.ServerRestarting
	case state.isstopping:
		sint = ifaces.ServerStopping
	case state.isstarting:
		sint = ifaces.ServerStarting
	case s.IsUp():
		sint = ifaces.ServerOnline
	}

	if state.iscrashed {
		sint = ifaces.ServerCrashedOffline + sint
	}

	return sint
}
