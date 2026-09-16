package giveaway

import (
	"fmt"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var SlashCommand = &discordgo.ApplicationCommand{
	Name:                     "giveaway",
	Description:              "Manage and create server giveaways",
	DefaultMemberPermissions: helpers.Ptr(int64(discordgo.PermissionManageGuild)),
	Options: []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "start",
			Description: "Start a new giveaway with options",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "prize",
					Description: "The prize for the giveaway",
					Required:    true,
					MaxLength:   256,
				},
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "duration",
					Description:  "Duration of the giveaway (e.g. 10m, 1h, 2d)",
					Required:     true,
					MaxLength:    32,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "winners",
					Description: "Number of winners to select (default: 1)",
					Required:    false,
					MinValue:    helpers.Ptr(1.0),
					MaxValue:    50,
				},
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel",
					Description: "The channel to post the giveaway in (default: current channel)",
					Required:    false,
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildText,
						discordgo.ChannelTypeGuildNews,
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "required_role",
					Description: "Require entrants to hold this role",
					Required:    false,
				},
				{
					Type:        discordgo.ApplicationCommandOptionRole,
					Name:        "blacklisted_role",
					Description: "Prevent members with this role from entering",
					Required:    false,
				},
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "min_account_age",
					Description:  "Minimum Discord account age required to enter (e.g. 7d, 30d)",
					Required:     false,
					MaxLength:    32,
					Autocomplete: true,
				},
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "min_server_tenure",
					Description:  "Minimum time on the server required to enter (e.g. 3d, 14d)",
					Required:     false,
					MaxLength:    32,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "min_level",
					Description: "Minimum server level required to enter",
					Required:    false,
					MinValue:    helpers.Ptr(1.0),
				},
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "host",
					Description: "Custom host user display (default: yourself)",
					Required:    false,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "end",
			Description: "End an active giveaway immediately and select winner(s)",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "message_id",
					Description:  "The giveaway to end (pick from the list)",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "reroll",
			Description: "Reroll new winner(s) for an ended giveaway",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "message_id",
					Description:  "The ended giveaway to reroll (pick from the list)",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "winners",
					Description: "Number of winners to reroll (default: 1)",
					Required:    false,
					MinValue:    helpers.Ptr(1.0),
					MaxValue:    50,
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "list",
			Description: "List all active and ended giveaways in the server",
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "cancel",
			Description: "Cancel an active giveaway without selecting winners",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "message_id",
					Description:  "The giveaway to cancel (pick from the list)",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	},
}

func RegisterGiveawaySlashCommands(s *discordgo.Session, appID, guildID string) error {
	if s == nil || appID == "" {
		return fmt.Errorf("invalid session or application ID")
	}

	if guildID != "" {
		if globals, err := s.ApplicationCommands(appID, ""); err != nil {
			bot.Warnf("[GIVEAWAY] Failed to list global commands for cleanup: %v", err)
		} else {
			for _, cmd := range globals {
				if err := s.ApplicationCommandDelete(appID, "", cmd.ID); err != nil {
					bot.Warnf("[GIVEAWAY] Failed to delete stale global command %s: %v", cmd.Name, err)
				}
			}
		}
	}

	_, err := s.ApplicationCommandCreate(appID, guildID, SlashCommand)
	if err != nil {
		bot.Errorf("[GIVEAWAY] Failed to register /giveaway slash command: %v", err)
		return err
	}

	bot.Infof("[GIVEAWAY] Registered /giveaway application slash command")
	return nil
}
