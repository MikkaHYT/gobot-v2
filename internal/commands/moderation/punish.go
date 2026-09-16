package moderation

import (
	"errors"
	"fmt"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/modlog"
	"gobot/internal/policy/auth"

	"github.com/bwmarrin/discordgo"
)

const (
	dmColorPunishment = helpers.ColorError
	dmColorReversal   = helpers.ColorSuccess
	dmColorTimeout    = helpers.ColorWarn
)

type punishment struct {
	caseType    string
	modlogLabel string
	reason      string
	duration    string
	embedTitle  string
	dm          *punishmentDM
	notifyFirst bool
}

type punishmentDM struct {
	Title    string
	Color    int
	Duration string
	Dispute  bool
}

type dmContext struct {
	guildName     string
	guildIcon     string
	moderatorName string
	moderatorAv   string
	reason        string
}

func executePunishment(ctx *bot.Context, p punishment, target *discordgo.User, member *discordgo.Member, action func() error, successDesc string) error {
	if err := checkTargetAuthority(ctx, target, member); err != nil {
		return ctx.SendError(err.Error())
	}

	var notice *dmContext
	var dmMsg *discordgo.Message
	var dmErr error
	if p.dm != nil {
		guildName, guildIcon := getGuildInfo(ctx)
		notice = &dmContext{
			guildName:     guildName,
			guildIcon:     guildIcon,
			moderatorName: ctx.Message.Author.String(),
			moderatorAv:   helpers.UserAvatar(ctx.Message.Author),
			reason:        p.reason,
		}
		if p.notifyFirst {
			dmMsg, dmErr = sendPunishmentDM(ctx.Session, target, *p.dm, *notice)
		}
	}

	if action != nil {
		if err := action(); err != nil {
			if dmMsg != nil {
				_ = ctx.Session.ChannelMessageDelete(dmMsg.ChannelID, dmMsg.ID)
			}
			return err
		}
	}

	_ = ctx.ReactSuccess()

	var caseID int64
	id, errCase := ctx.DB.CreateModCase(ctx.Message.GuildID, target.ID, ctx.Message.Author.ID, p.caseType, p.reason)
	if errCase != nil {
		bot.Errorf("[MODLOG] Failed to create database mod case for %s: %v", target.String(), errCase)
	} else {
		caseID = id
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.PunishmentEvent{
		GuildID:    ctx.Message.GuildID,
		Action:     p.modlogLabel,
		TargetUser: target,
		Moderator:  ctx.Message.Author,
		Duration:   p.duration,
		Reason:     p.reason,
		CaseID:     caseID,
	})

	if notice != nil && !p.notifyFirst {
		_, dmErr = sendPunishmentDM(ctx.Session, target, *p.dm, *notice)
	}

	desc := successDesc
	if dmErr != nil {
		desc += fmt.Sprintf("\n%s, could not PM %s (%s)", p.modlogLabel, target.Username, target.ID)
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       p.embedTitle,
		Description: desc,
	})
	return err
}

func sendPunishmentDM(s *discordgo.Session, target *discordgo.User, dm punishmentDM, info dmContext) (*discordgo.Message, error) {
	if target == nil || target.Bot {
		return nil, nil
	}
	ch, err := s.UserChannelCreate(target.ID)
	if err != nil {
		return nil, err
	}

	footer := "If you would like to dispute this punishment, contact a staff member."
	if !dm.Dispute {
		footer = fmt.Sprintf("Notification from %s", info.guildName)
	}

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    info.guildName,
			IconURL: info.guildIcon,
		},
		Title: dm.Title,
		Color: dm.Color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Server", Value: info.guildName, Inline: true},
			{Name: "Moderator", Value: info.moderatorName, Inline: true},
			{Name: "Reason", Value: info.reason, Inline: true},
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: footer},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if dm.Duration != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Duration",
			Value:  dm.Duration,
			Inline: true,
		})
	}
	if info.moderatorAv != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: info.moderatorAv}
	}
	return s.ChannelMessageSendEmbed(ch.ID, embed)
}

func getGuildInfo(ctx *bot.Context) (name string, iconURL string) {
	if ctx == nil {
		return "", ""
	}
	if guild, err := ctx.Guild(); err == nil && guild != nil {
		return guild.Name, guild.IconURL("256")
	}
	return "", ""
}

func checkTargetAuthority(ctx *bot.Context, target *discordgo.User, member *discordgo.Member) error {
	if member == nil && target != nil && ctx.Message != nil && ctx.Message.GuildID != "" {
		m, err := ctx.GetMember(target.ID)
		if err == nil && m != nil {
			member = m
		} else if err != nil && !helpers.IsDiscordNotFound(err) {
			return fmt.Errorf("could not verify member hierarchy status: %w", err)
		}
	}
	if member == nil {
		return nil
	}

	targetID := ""
	if member.User != nil {
		targetID = member.User.ID
	} else if target != nil {
		targetID = target.ID
	}

	d := ctx.Authorizer().Evaluate(ctx.Context(), auth.Request{
		GuildID:      ctx.Message.GuildID,
		ChannelID:    ctx.Message.ChannelID,
		ActorID:      ctx.Message.Author.ID,
		ActorMember:  ctx.Message.Member,
		TargetMember: member,
		TargetUserID: targetID,
		CheckBot:     true,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return errors.New(d.Reason)
		}
		if d.Err != nil {
			return d.Err
		}
		return errors.New("you cannot moderate this user")
	}
	return nil
}

func checkRoleAuthority(ctx *bot.Context, roleID string) error {
	if ctx.Message == nil || ctx.Message.GuildID == "" {
		return errors.New("guild context is required")
	}
	d := ctx.Authorizer().Evaluate(ctx.Context(), auth.Request{
		GuildID:      ctx.Message.GuildID,
		ChannelID:    ctx.Message.ChannelID,
		ActorID:      ctx.Message.Author.ID,
		ActorMember:  ctx.Message.Member,
		TargetRoleID: roleID,
		CheckBot:     true,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return errors.New(d.Reason)
		}
		if d.Err != nil {
			return d.Err
		}
		return errors.New("you cannot modify or assign this role")
	}
	return nil
}
