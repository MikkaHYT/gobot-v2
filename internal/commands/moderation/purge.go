package moderation

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gobot/internal/bot"

	"github.com/bwmarrin/discordgo"
)

type PurgeFilter struct {
	TargetUserID string
	OnlyBots     bool
	OnlyHumans   bool
	OnlyLinks    bool
	OnlyFiles    bool
	ContainsText string
	Prefix       string
}

func ExecutePurge(ctx *bot.Context, amount int, filter PurgeFilter) (int, error) {
	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionManageMessages) {
		return 0, fmt.Errorf("permission check failed")
	}

	if amount <= 0 {
		return 0, fmt.Errorf("please specify a message amount greater than 0")
	}

	if amount > 100 {
		amount = 100
	}

	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, ctx.Message.ID)

	fetchLimit := amount + 1
	if fetchLimit > 100 {
		fetchLimit = 100
	}
	if filter.TargetUserID != "" || filter.OnlyBots || filter.OnlyHumans || filter.OnlyLinks || filter.OnlyFiles || filter.ContainsText != "" {
		fetchLimit = 100
	}

	messages, err := ctx.Session.ChannelMessages(ctx.Message.ChannelID, fetchLimit, "", "", "")
	if err != nil {
		return 0, fmt.Errorf("failed to fetch channel messages: %w", err)
	}

	fourteenDaysAgo := time.Now().Add(-14 * 24 * time.Hour)
	var messageIDsToDelete []string

	for _, msg := range messages {
		if msg.ID == ctx.Message.ID {
			continue
		}

		if time.Time(msg.Timestamp).Before(fourteenDaysAgo) {
			continue
		}

		if filter.TargetUserID != "" && (msg.Author == nil || msg.Author.ID != filter.TargetUserID) {
			continue
		}

		if filter.OnlyBots {
			isBot := msg.Author != nil && msg.Author.Bot
			isCmd := filter.Prefix != "" && strings.HasPrefix(msg.Content, filter.Prefix)
			if !isBot && !isCmd {
				continue
			}
		}

		if filter.OnlyHumans {
			if msg.Author == nil || msg.Author.Bot {
				continue
			}
		}

		if filter.OnlyLinks {
			lower := strings.ToLower(msg.Content)
			if !strings.Contains(lower, "http://") && !strings.Contains(lower, "https://") && !strings.Contains(lower, "discord.gg") {
				continue
			}
		}

		if filter.OnlyFiles {
			if len(msg.Attachments) == 0 && len(msg.Embeds) == 0 {
				continue
			}
		}

		if filter.ContainsText != "" {
			if !strings.Contains(strings.ToLower(msg.Content), strings.ToLower(filter.ContainsText)) {
				continue
			}
		}

		messageIDsToDelete = append(messageIDsToDelete, msg.ID)
		if len(messageIDsToDelete) >= amount {
			break
		}
	}

	if len(messageIDsToDelete) == 0 {
		return 0, nil
	}

	if len(messageIDsToDelete) == 1 {
		err = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, messageIDsToDelete[0])
	} else {
		err = ctx.Session.ChannelMessagesBulkDelete(ctx.Message.ChannelID, messageIDsToDelete)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to delete messages: %w", err)
	}

	return len(messageIDsToDelete), nil
}

type PurgeCmd struct{}

func (c *PurgeCmd) Name() string        { return "purge" }
func (c *PurgeCmd) Aliases() []string   { return []string{"clear", "clean", "prune"} }
func (c *PurgeCmd) Category() string    { return "Moderation" }
func (c *PurgeCmd) Description() string { return "Bulk deletes messages from the channel." }
func (c *PurgeCmd) Usage() string       { return "[links|files|humans|contains <text>|@user] [amount]" }
func (c *PurgeCmd) Example() string     { return "50" }
func (c *PurgeCmd) Permissions() int64  { return discordgo.PermissionManageMessages }

func (c *PurgeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	filter := PurgeFilter{}
	firstArg := strings.ToLower(ctx.Args[0])
	amount := 0
	targetUserID := ""

	switch firstArg {
	case "links", "link", "urls", "url":
		filter.OnlyLinks = true
		amount = 50
		if len(ctx.Args) > 1 {
			if a, parseErr := strconv.Atoi(ctx.Args[1]); parseErr == nil && a > 0 {
				amount = a
			}
		}
	case "files", "file", "images", "image", "attachments", "attachment", "media":
		filter.OnlyFiles = true
		amount = 50
		if len(ctx.Args) > 1 {
			if a, parseErr := strconv.Atoi(ctx.Args[1]); parseErr == nil && a > 0 {
				amount = a
			}
		}
	case "humans", "human", "users", "user":
		filter.OnlyHumans = true
		amount = 50
		if len(ctx.Args) > 1 {
			if a, parseErr := strconv.Atoi(ctx.Args[1]); parseErr == nil && a > 0 {
				amount = a
			}
		}
	case "bots", "bot":
		filter.OnlyBots = true
		filter.Prefix = ctx.Prefix
		amount = 50
		if len(ctx.Args) > 1 {
			if a, parseErr := strconv.Atoi(ctx.Args[1]); parseErr == nil && a > 0 {
				amount = a
			}
		}
	case "contains", "match", "find":
		if len(ctx.Args) < 2 {
			return ctx.SendError(fmt.Sprintf("Please specify text to match. Example: `%spurge contains spam 50`", ctx.Prefix))
		}
		filter.ContainsText = ctx.Args[1]
		amount = 50
		if len(ctx.Args) > 2 {
			if a, parseErr := strconv.Atoi(ctx.Args[2]); parseErr == nil && a > 0 {
				amount = a
			}
		}
	default:
		if len(ctx.Message.Mentions) > 0 {
			targetUserID = ctx.Message.Mentions[0].ID
			for _, arg := range ctx.Args {
				if a, parseErr := strconv.Atoi(arg); parseErr == nil && a > 0 {
					amount = a
					break
				}
			}
		} else if len(ctx.Args) == 1 {
			if a, parseErr := strconv.Atoi(ctx.Args[0]); parseErr == nil && a > 0 {
				amount = a
			} else {
				return ctx.SendError(fmt.Sprintf("Please specify a valid message amount (1-100). Example: `%spurge 50`", ctx.Prefix))
			}
		} else if len(ctx.Args) >= 2 {
			var userQuery string
			for _, arg := range ctx.Args {
				if a, parseErr := strconv.Atoi(arg); parseErr == nil && a > 0 && amount == 0 {
					amount = a
				} else if userQuery == "" {
					userQuery = arg
				}
			}

			if userQuery != "" {
				u, _, err := ctx.ResolveUserAndMember(userQuery)
				if err != nil {
					return ctx.SendError(fmt.Sprintf("User `%s` not found.", userQuery))
				}
				targetUserID = u.ID
			}
		}
		filter.TargetUserID = targetUserID
	}

	if amount <= 0 {
		return ctx.SendError("Please specify a valid message amount (1-100).")
	}

	requestedAmount := amount
	if amount > 100 {
		amount = 100
	}

	count, err := ExecutePurge(ctx, amount, filter)
	if err != nil {
		return err
	}

	desc := fmt.Sprintf("Purged **%d** message(s).", count)
	if filter.TargetUserID != "" {
		desc = fmt.Sprintf("Purged **%d** message(s) from <@%s>.", count, filter.TargetUserID)
	} else if filter.OnlyLinks {
		desc = fmt.Sprintf("Purged **%d** link message(s).", count)
	} else if filter.OnlyFiles {
		desc = fmt.Sprintf("Purged **%d** file message(s).", count)
	} else if filter.OnlyHumans {
		desc = fmt.Sprintf("Purged **%d** human message(s).", count)
	} else if filter.OnlyBots {
		desc = fmt.Sprintf("Purged **%d** bot message(s).", count)
	} else if filter.ContainsText != "" {
		desc = fmt.Sprintf("Purged **%d** message(s) containing \"%s\".", count, filter.ContainsText)
	}

	embed := &discordgo.MessageEmbed{
		Description: desc,
	}
	if requestedAmount > 100 {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: "Purge limit is 100 messages.",
		}
	}

	return ctx.SendSelfDeletingEmbedObject(embed)
}

type BotclearCmd struct{}

func (c *BotclearCmd) Name() string      { return "botclear" }
func (c *BotclearCmd) Aliases() []string { return []string{"bc", "clearbots"} }
func (c *BotclearCmd) Category() string  { return "Moderation" }
func (c *BotclearCmd) Description() string {
	return "Deletes bot messages and bot invocations."
}
func (c *BotclearCmd) Usage() string      { return "<amount>" }
func (c *BotclearCmd) Example() string    { return "50" }
func (c *BotclearCmd) Permissions() int64 { return discordgo.PermissionManageMessages }

func (c *BotclearCmd) Execute(ctx *bot.Context) error {
	amount := 50
	if len(ctx.Args) > 0 {
		a, parseErr := strconv.Atoi(ctx.Args[0])
		if parseErr != nil || a <= 0 {
			return ctx.SendError(fmt.Sprintf("Please specify a valid number of messages to clear (e.g. `%sbotclear 25`).", ctx.Prefix))
		}
		amount = a
	}

	requestedAmount := amount
	if amount > 100 {
		amount = 100
	}

	count, err := ExecutePurge(ctx, amount, PurgeFilter{
		OnlyBots: true,
		Prefix:   ctx.Prefix,
	})
	if err != nil {
		return err
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Purged **%d** bot message(s).", count),
	}
	if requestedAmount > 100 {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: "Purge limit is 100 messages.",
		}
	}

	return ctx.SendSelfDeletingEmbedObject(embed)
}
