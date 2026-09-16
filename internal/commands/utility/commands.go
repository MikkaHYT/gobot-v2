package utility

import (
	"gobot/internal/commands"
	"gobot/internal/commands/utility/expressions"
)

var HelpCmdInstance = &HelpCmd{}

var Commands = []commands.Command{
	HelpCmdInstance,
	&AfkCmd{},
	&ServerAvatarCmd{},
	&AvatarCmd{},
	&BannerCmd{},
	&ServerBannerCmd{},
	&UserinfoCmd{},
	&DetailedUserinfoCmd{},
	&RoleInfoCMD{},
	&HealthCmd{},
	&PingCmd{},
	&GayCmd{},
	&CatCmd{},
	&BunnyCmd{},
	&TagCmd{},
	&GrailCmd{},
	&UrbanCmd{},
	&ImageCmd{},
	&DefineCmd{},
	&DogCmd{},
	&EmbedCmd{},
	&EditembedCmd{},
	&QuoteCmd{},
	&EightBallCmd{},
	&QuickpollCmd{},
	&PollCmd{},
	&OcrCmd{},
	&TranslateCmd{},
	&ServerinfoCmd{},
	&DetailedServerinfoCmd{},
	&expressions.EmojiCmd{},
	&expressions.StickerCmd{},
	&expressions.EnlargeCmd{},
	&expressions.StealCmd{},
}
