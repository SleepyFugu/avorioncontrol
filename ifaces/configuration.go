package ifaces

import (
	"avorioncontrol/logger"
	"time"
)

// IConfigurator describes an interface to our configuration backend
type IConfigurator interface {
	Validate() error

	IDatabaseConfigurator
	IDiscordConfigurator
	ICommandConfigurator
	IGalaxyConfigurator
	IEventConfigurator
	IAuthConfigurator
	IGameConfigurator
	ITimeConfigurator
	IChatConfigurator
	IConfigSaveLoader
	IModConfigurator
	logger.ILogger
}

// IDiscordConfigurator describes an interface that describes Discord configurations
type IDiscordConfigurator interface {
	Token() string

	BotsAllowed() bool
	DiscordLink() string
	SetDiscordLink(string)
	SetBotsAllowed(bool)
	StatusChannel() (string, bool)
	SetStatusChannel(string)
	StatusChannelClear() bool
}

// IGameConfigurator describes an interface to a games configuration
type IGameConfigurator interface {
	RCONBin() string
	RCONPort() int
	DataPath() string
	RCONAddr() string
	RCONPass() string
	InstallPath() string
	LoadGameConfig() error
	GameConfig() (*ServerGameConfig, bool)
	PostUpCommand() string
	PostDownCommand() string
	HangTimeDuration() time.Duration
	DBUpdateTimeDuration() time.Duration
}

// IGalaxyConfigurator describes an interface to an object that can configure a
//	galaxy
type IGalaxyConfigurator interface {
	SetGalaxy(string)
	Galaxy() string
}

// ICommandConfigurator describes an interface to an object that can configure
//	bot commands
type ICommandConfigurator interface {
	DisableCommand(string) error
	CommandDisabled(string) bool

	SetAliasCommand(string, string) error
	GetAliasedCommand(string) (bool, string)
	CommandAliases(string) (bool, []string)

	SetPrefix(string)
	Prefix() string

	SetToken(string)
	Token() string
}

// IChatConfigurator describes an interface to an object that can configure chats
type IChatConfigurator interface {
	ChatPipe() chan ChatData
	SetChatChannel(string) chan ChatData
	ChatChannel() string
	ReactConfirm() bool
}

// ITimeConfigurator describes an interface to the configured timezone
type ITimeConfigurator interface {
	TimeZone() string
	SetTimeZone(string) error
}

// IAuthConfigurator describes an interface to an authorization object
type IAuthConfigurator interface {
	AddRoleAuth(string, int) error
	RemoveRoleAuth(string) error
	GetRoleAuth(string) int

	AddCmndAuth(string, int)
	GetCmndAuth(string) int
	RemoveCmndAuth(string) error
}

// IConfigSaveLoader describes an interface to a an object that saves
// and loads configuration
type IConfigSaveLoader interface {
	LoadConfiguration() error
	SaveConfiguration() error
}

// IDatabaseConfigurator describes an interface to a db configurator
type IDatabaseConfigurator interface {
	DBName() string
}

// IModConfigurator describes an interface to a modconfig builder
type IModConfigurator interface {
	BuildModConfig() error
	AddServerMod(int64) error
	RemoveServerMod(int64) error
	AddClientMod(int64) error
	RemoveClientMod(int64) error
	ListServerMods() []int64
	ListClientMods() []int64
}

// IEventConfigurator describes a configuration object that has LoggedServerEvents
type IEventConfigurator interface {
	GetEvents() []*LoggedServerEvent
	LogPipe() chan ChatData
	SetLogChannel(string) chan ChatData
	LogChannel() string
}
