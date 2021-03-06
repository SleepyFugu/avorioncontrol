package avorion

import (
	"avorioncontrol/avorion/events"
	"avorioncontrol/logger"
	"bufio"
	"time"
)

/**************/
/* Goroutines */
/**************/

// updateAvorionStatus is the goroutine responsible for making sure that the
// server is still accessible, and restarting it when needed. In addition, this
// goroutine also updates various server related data values at set intervals
func updateAvorionStatus(s *Server, closech chan struct{}) {
	defer s.wg.Done()
	defer func() { logger.LogInfo(s, "Stopping old status supervisor") }()
	s.wg.Add(1)

	logger.LogInit(s, "Starting status supervisor")
	for {
		// Close the routine gracefully
		select {
		case <-s.exit:
			s.Stop(false)
			return

		case <-closech:
			if !state.isstopping && !state.isrestarting && !state.isstarting {
				logger.LogWarning(s, "Avorion server exited abnormally, restarting")
				s.Crashed()
				if err := s.Restart(); err == nil {
					state.iscrashed = false
					state.isrestarting = false
				}
			}
			return

		// Check the server status after the configured duration of time has passed
		case <-time.After(s.config.HangTimeDuration()):
			online := 0
			for _, p := range s.players {
				if p.Online() {
					online++
				}
			}

			s.onlineplayercount = online

			if state.isrestarting || state.isstopping || state.isstarting {
				continue
			}

			// TODO: Make this command configura
			_, err := s.RunCommand("echo Server status check")
			if err != nil {
				s.Crashed()
				logger.LogError(s, err.Error())
				s.Cmd.Process.Kill()
			}

			if s.IsCrashed() && err == nil {
				s.Recovered()
			}

		// Update our playerinfo db after the configured duration of time has passed
		case <-time.After(s.config.DBUpdateTimeDuration()):
			s.UpdatePlayerDatabase(true)
		}
	}
}

// superviseAvorionOut watches the output provided by the Avorion process and
// applies the applicable eventHandler for the output recieved. This routine is
// also responsible for sending the stdout of Avorion to the output channel
// to be processed by our websocket handler.
func superviseAvorionOut(s *Server, ready chan struct{},
	closech chan struct{}) {
	defer func() { logger.LogInfo(s, "Stopping old output supervisor") }()
	logger.LogInit(s, "Started Avorion stdout supervisor")

	scanner := bufio.NewScanner(s.stdout)

	// TODO: Move the scanner.Scan() loop into a goroutine.
	for scanner.Scan() {
		out := scanner.Text()

		// Exit gracefully
		select {
		case <-closech:
			return

		// Once we're ready, start processing logs.
		case <-ready:
			e := events.GetFromString(out)
			if e == nil {
				logger.LogOutput(s, out)
				continue
			}
			e.Handler(s, e, out, nil)

		// Output as INIT until the server is ready
		default:
			switch out {
			case "Server startup complete.":
				logger.LogInit(s, "Avorion server initialization completed")
				close(ready)

			case "Server startup FAILED.":
				logger.LogError(s, "Server startup FAILED.")
				s.Crashed()

			default:
				e := events.GetFromString(out)
				if e == nil {
					continue
				}
				e.Handler(s, e, out, nil)
			}
		}
	}
}
