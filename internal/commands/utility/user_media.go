package utility

import (
	"gobot/internal/bot"

	"github.com/bwmarrin/discordgo"
)

type AvatarCmd struct{}

func (c *AvatarCmd) Name() string        { return "avatar" }
func (c *AvatarCmd) Aliases() []string   { return []string{"av"} }
func (c *AvatarCmd) Category() string    { return "Media & Fun" }
func (c *AvatarCmd) Description() string { return "Displays the global avatar of a user." }
func (c *AvatarCmd) Usage() string       { return "[user | userid]" }
func (c *AvatarCmd) Example() string     { return "@Cloudyy" }

func (c *AvatarCmd) Execute(ctx *bot.Context) error {
	targetUser, targetMember, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user with that ID or mention.")
	}

	media := GetUserMediaInfo(targetUser, targetMember, MediaGlobalAvatar)

	embed := &discordgo.MessageEmbed{
		Title: media.Title,
		Image: &discordgo.MessageEmbedImage{
			URL: media.URL,
		},
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ServerAvatarCmd struct{}

func (c *ServerAvatarCmd) Name() string      { return "serveravatar" }
func (c *ServerAvatarCmd) Aliases() []string { return []string{"sav"} }
func (c *ServerAvatarCmd) Category() string  { return "Media & Fun" }
func (c *ServerAvatarCmd) Description() string {
	return "Displays the server-specific avatar of a user."
}
func (c *ServerAvatarCmd) Usage() string   { return "[user | userid]" }
func (c *ServerAvatarCmd) Example() string { return "@Cloudyy" }

func (c *ServerAvatarCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	targetUser, targetMember, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user with that ID or mention.")
	}

	media := GetUserMediaInfo(targetUser, targetMember, MediaServerAvatar)

	embed := &discordgo.MessageEmbed{
		Title:       media.Title,
		Description: media.NoticeNote,
		Image: &discordgo.MessageEmbedImage{
			URL: media.URL,
		},
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type BannerCmd struct{}

func (c *BannerCmd) Name() string        { return "banner" }
func (c *BannerCmd) Aliases() []string   { return []string{} }
func (c *BannerCmd) Category() string    { return "Media & Fun" }
func (c *BannerCmd) Description() string { return "Displays the global banner of a user." }
func (c *BannerCmd) Usage() string       { return "[user | userid]" }
func (c *BannerCmd) Example() string     { return "@Cloudyy" }

func (c *BannerCmd) Execute(ctx *bot.Context) error {
	targetUser, targetMember, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user with that ID or mention.")
	}

	fetchedUser, errUser := ctx.Session.User(targetUser.ID)
	if errUser == nil && fetchedUser != nil {
		targetUser = fetchedUser
	}

	media := GetUserMediaInfo(targetUser, targetMember, MediaGlobalBanner)

	embed := &discordgo.MessageEmbed{
		Title:       media.Title,
		Description: media.NoticeNote,
	}
	if media.URL != "" {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: media.URL,
		}
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ServerBannerCmd struct{}

func (c *ServerBannerCmd) Name() string      { return "serverbanner" }
func (c *ServerBannerCmd) Aliases() []string { return []string{"sbanner"} }
func (c *ServerBannerCmd) Category() string  { return "Media & Fun" }
func (c *ServerBannerCmd) Description() string {
	return "Displays the server-specific banner of a user."
}
func (c *ServerBannerCmd) Usage() string   { return "[user | userid]" }
func (c *ServerBannerCmd) Example() string { return "@Cloudyy" }

func (c *ServerBannerCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	targetUser, targetMember, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user with that ID or mention.")
	}

	media := GetUserMediaInfo(targetUser, targetMember, MediaServerBanner)

	embed := &discordgo.MessageEmbed{
		Title:       media.Title,
		Description: media.NoticeNote,
	}
	if media.URL != "" {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: media.URL,
		}
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}
