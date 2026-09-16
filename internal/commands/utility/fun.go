package utility

import (
	"fmt"
	"math/rand"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var eightBallResponses = []string{
	"It is certain.", "It is decidedly so.", "Without a doubt.", "Yes definitely.",
	"You may rely on it.", "As I see it, yes.", "Most likely.", "Outlook good.",
	"Yes.", "Signs point to yes.", "Reply hazy, try again.", "Ask again later.",
	"Better not tell you now.", "Cannot predict now.", "Concentrate and ask again.",
	"Don't count on it.", "My reply is no.", "My sources say no.",
	"Outlook not so good.", "Very doubtful.",
}

type GayCmd struct{}

func (c *GayCmd) Name() string        { return "gay" }
func (c *GayCmd) Aliases() []string   { return []string{"howgay"} }
func (c *GayCmd) Category() string    { return "Media & Fun" }
func (c *GayCmd) Description() string { return "Calculates gay percentage for a user." }
func (c *GayCmd) Usage() string       { return "(@user)" }
func (c *GayCmd) Example() string     { return "@Cloudyy" }

func (c *GayCmd) Execute(ctx *bot.Context) error {
	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user.")
	}

	percentage := rand.Intn(101)
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("**<@%s>** is **%d%%** gay.", targetUser.ID, percentage),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type EightBallCmd struct{}

func (c *EightBallCmd) Name() string      { return "8ball" }
func (c *EightBallCmd) Aliases() []string { return []string{"eightball", "ask"} }
func (c *EightBallCmd) Category() string  { return "Media & Fun" }
func (c *EightBallCmd) Description() string {
	return "truthfully answers any and all answers with 100% correctness"
}
func (c *EightBallCmd) Usage() string   { return "<question>" }
func (c *EightBallCmd) Example() string { return "are clouds real?" }

func (c *EightBallCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	question := strings.Join(ctx.Args, " ")
	answer := eightBallResponses[rand.Intn(len(eightBallResponses))]

	embed := &discordgo.MessageEmbed{
		Title:       "Magic 8-Ball",
		Description: fmt.Sprintf("**Question:** %s\n**Answer:** %s", question, answer),
		Color:       helpers.ColorDefault,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

type QuickpollCmd struct{}

func (c *QuickpollCmd) Name() string      { return "quickpoll" }
func (c *QuickpollCmd) Aliases() []string { return []string{"qp"} }
func (c *QuickpollCmd) Category() string  { return "Utility" }
func (c *QuickpollCmd) Description() string {
	return "Add 👍 and 👎 reactions directly to your message."
}
func (c *QuickpollCmd) Usage() string   { return "[message]" }
func (c *QuickpollCmd) Example() string { return "is the earth a sphere" }

func (c *QuickpollCmd) Execute(ctx *bot.Context) error {
	targetMsgID := ctx.Message.ID
	if len(ctx.Args) == 0 {
		if ref := ctx.Message.ReferencedMessage; ref != nil && ref.ID != "" {
			targetMsgID = ref.ID
		} else if ref := ctx.Message.MessageReference; ref != nil && ref.MessageID != "" {
			targetMsgID = ref.MessageID
		}
	}

	addVoteReactions(ctx.Session, ctx.Message.ChannelID, targetMsgID)
	return nil
}

type PollCmd struct{}

func (c *PollCmd) Name() string      { return "poll" }
func (c *PollCmd) Aliases() []string { return []string{"vote"} }
func (c *PollCmd) Category() string  { return "Utility" }
func (c *PollCmd) Description() string {
	return "Create a clean poll message with 👍 and 👎 reactions."
}
func (c *PollCmd) Usage() string   { return "<question>" }
func (c *PollCmd) Example() string { return "is the earth a sphere" }

func (c *PollCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	question := strings.Join(ctx.Args, " ")
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("**%s**", question),
		Color:       helpers.ColorDefault,
		Footer: &discordgo.MessageEmbedFooter{
			Text:    fmt.Sprintf("Poll by %s", ctx.Message.Author.Username),
			IconURL: helpers.UserAvatar(ctx.Message.Author),
		},
	}

	msg, err := ctx.ReplyEmbed(embed)
	if err != nil {
		return err
	}

	addVoteReactions(ctx.Session, msg.ChannelID, msg.ID)
	return nil
}

func addVoteReactions(s *discordgo.Session, channelID, messageID string) {
	_ = s.MessageReactionAdd(channelID, messageID, "👍")
	_ = s.MessageReactionAdd(channelID, messageID, "👎")
}
