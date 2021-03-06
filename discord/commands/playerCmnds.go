package commands

import (
	"avorioncontrol/ifaces"
	"avorioncontrol/logger"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func playerKickCmnd(s *discordgo.Session, m *discordgo.MessageCreate, a BotArgs,
	c ifaces.IConfigurator, cmd *CommandRegistrant) (*CommandOutput, ICommandError) {

	var (
		reason = `Kicked by an Admin`
		reg    = cmd.Registrar()
		srv    = reg.server
		out    = newCommandOutput(cmd, "Kick Player")

		obj ifaces.IPlayer
	)

	out.Quoted = true

	if !HasNumArgs(a[1:], 1, -1) {
		return nil, &ErrInvalidArgument{
			message: "Please provide a player index to kick",
			cmd:     cmd}
	}

	ref := a[2]

	if len(a) > 2 {
		reason = strings.Join(a[3:], " ")
	}

	if p := srv.Player(ref); p != nil {
		obj = p
	}

	if obj != nil {
		obj.Kick(reason)
		logger.LogInfo(cmd, sprintf("[%s] kicked [%s]", m.Author.String(),
			obj.Name()))
		out.AddLine(sprintf("Kicked player %s", obj.Name()))
		out.Construct()
		return out, nil
	}

	return nil, &ErrInvalidArgument{
		message: sprintf("%s is an invalid reference to a player", ref),
		cmd:     cmd}
}

func playerBanCmnd(s *discordgo.Session, m *discordgo.MessageCreate, a BotArgs,
	c ifaces.IConfigurator, cmd *CommandRegistrant) (*CommandOutput, ICommandError) {
	var (
		reason = `Banned by an Admin`
		reg    = cmd.Registrar()
		srv    = reg.server
		out    = newCommandOutput(cmd, "Kick Player")

		obj ifaces.IPlayer
	)

	out.Quoted = true

	if !HasNumArgs(a, 1, -1) {
		return nil, &ErrInvalidArgument{
			message: "Please provide a player index to kick",
			cmd:     cmd}
	}

	ref := a[2]

	if len(a) > 2 {
		reason = strings.Join(a[3:], " ")
	}

	if p := srv.Player(ref); p != nil {
		obj = p
	}

	if obj != nil {
		obj.Ban(reason)
		logger.LogInfo(cmd, sprintf("[%s] banned [%s]", m.Author.String(),
			obj.Name()))
		out.AddLine(sprintf("Banned player %s", obj.Name()))
		out.Construct()
		return out, nil
	}

	return nil, &ErrInvalidArgument{
		message: sprintf("%s is an invalid reference to a player", ref),
		cmd:     cmd}
}

func showOnlinePlayersCmnd(s *discordgo.Session, m *discordgo.MessageCreate, a BotArgs,
	c ifaces.IConfigurator, cmd *CommandRegistrant) (*CommandOutput, ICommandError) {
	var (
		reg = cmd.Registrar()
		srv = reg.server
		out = newCommandOutput(cmd, "Players Online")
		cnt = 0
	)

	out.Quoted = true

	for _, p := range srv.Players() {
		if p.Online() {
			cnt++
			out.AddLine(sprintf("**%s**: `%s`", p.Index(), p.Name()))
		}
	}

	if cnt == 0 {
		out.AddLine("No players online")
		out.Construct()
		return out, nil
	}

	out.Construct()
	return out, nil
}
