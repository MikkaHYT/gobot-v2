package listeners

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/modlog"
	"gobot/internal/policy"
	"gobot/internal/policy/auth"

	"github.com/bwmarrin/discordgo"
)

var (
	autoModDeletedMu  sync.RWMutex
	autoModDeletedIDs = make(map[string]time.Time)
)

func MarkAutoModDeleted(msgID string) {
	if msgID == "" {
		return
	}
	autoModDeletedMu.Lock()
	defer autoModDeletedMu.Unlock()
	autoModDeletedIDs[msgID] = time.Now()
	now := time.Now()
	for id, t := range autoModDeletedIDs {
		if now.Sub(t) > time.Minute {
			delete(autoModDeletedIDs, id)
		}
	}
}

func IsAutoModDeleted(msgID string) bool {
	if msgID == "" {
		return false
	}
	autoModDeletedMu.RLock()
	defer autoModDeletedMu.RUnlock()
	_, exists := autoModDeletedIDs[msgID]
	return exists
}

func requiredPermForAction(punishment string) int64 {
	switch strings.ToLower(punishment) {
	case "timeout":
		return discordgo.PermissionModerateMembers
	case "kick":
		return discordgo.PermissionKickMembers
	case "ban":
		return discordgo.PermissionBanMembers
	default:
		return 0
	}
}

type punishmentParams struct {
	GuildID        string
	ChannelID      string
	Author         *discordgo.User
	Punishment     string
	Reason         string
	MatchedKeyword string
	Content        string
}

func executePunishment(s *discordgo.Session, db *database.DB, p punishmentParams) {
	normPunishment := strings.ToLower(p.Punishment)
	enforcementSucceeded := false

	if normPunishment == "delete" || normPunishment == "none" || normPunishment == "" {
		if db != nil {
			modlog.Log(s, db, &modlog.AutoModViolationEvent{
				GuildID:        p.GuildID,
				TargetUser:     p.Author,
				RuleName:       "Word / Content Filter",
				Action:         "Block Message",
				ChannelID:      p.ChannelID,
				MatchedKeyword: p.MatchedKeyword,
				Content:        p.Content,
			})
		}
		return
	}

	if normPunishment == "kick" || normPunishment == "ban" || normPunishment == "timeout" {
		adapter := auth.NewDiscordStateAdapter(s)
		botID := adapter.BotUserID()
		authorizer := auth.NewAuthorizer(adapter, nil)
		decision := authorizer.Evaluate(context.Background(), auth.Request{
			GuildID:      p.GuildID,
			ChannelID:    p.ChannelID,
			ActorID:      botID,
			TargetUserID: p.Author.ID,
			RequiredPerm: requiredPermForAction(normPunishment),
			CheckBot:     true,
		})
		if !decision.Allowed {
			logger.Warnf("[AUTOMOD] Skipped punishing %s in guild %s: %s", p.Author.String(), p.GuildID, decision.Reason)
			if db != nil {
				modlog.Log(s, db, &modlog.AutoModViolationEvent{
					GuildID:        p.GuildID,
					TargetUser:     p.Author,
					RuleName:       "Word / Content Filter (Hierarchy Bypass Prevented)",
					Action:         "none",
					ChannelID:      p.ChannelID,
					MatchedKeyword: p.MatchedKeyword,
					Content:        p.Content,
				})
			}
			return
		}
	}

	switch normPunishment {
	case "warn":
		embed := &discordgo.MessageEmbed{
			Title:       "AutoMod Warning",
			Description: fmt.Sprintf("%s, your message was removed: **%s**.", p.Author.Mention(), p.Reason),
			Color:       helpers.ColorWarn,
		}
		msg, err := s.ChannelMessageSendEmbed(p.ChannelID, embed)
		if err != nil || msg == nil {
			logger.Warnf("[AUTOMOD] Failed to send warning for %s in guild %s: %v", p.Author.String(), p.GuildID, err)
			break
		}
		enforcementSucceeded = true
		time.AfterFunc(helpers.DurationFeedbackLong, func() {
			_ = s.ChannelMessageDelete(p.ChannelID, msg.ID)
		})

	case "timeout":
		until := time.Now().Add(10 * time.Minute)
		if errTimeout := s.GuildMemberTimeout(p.GuildID, p.Author.ID, &until); errTimeout != nil {
			logger.Warnf("[AUTOMOD] Failed to timeout %s in guild %s: %v", p.Author.String(), p.GuildID, errTimeout)
		} else {
			enforcementSucceeded = true
		}

	case "kick":
		if errKick := s.GuildMemberDeleteWithReason(p.GuildID, p.Author.ID, "AutoMod: "+p.Reason); errKick != nil {
			logger.Warnf("[AUTOMOD] Failed to kick %s in guild %s: %v", p.Author.String(), p.GuildID, errKick)
		} else {
			enforcementSucceeded = true
		}

	case "ban":
		if errBan := s.GuildBanCreateWithReason(p.GuildID, p.Author.ID, "AutoMod: "+p.Reason, 1); errBan != nil {
			logger.Warnf("[AUTOMOD] Failed to ban %s in guild %s: %v", p.Author.String(), p.GuildID, errBan)
		} else {
			enforcementSucceeded = true
		}
	}

	if db != nil && enforcementSucceeded && (normPunishment == "kick" || normPunishment == "ban" || normPunishment == "timeout" || normPunishment == "warn") {
		botUser := s.State.User
		botUserID := "AutoMod"
		if botUser != nil && botUser.ID != "" {
			botUserID = botUser.ID
		}
		if _, errCase := db.CreateModCase(p.GuildID, p.Author.ID, botUserID, strings.ToUpper(normPunishment), "AutoMod: "+p.Reason); errCase != nil {
			logger.Warnf("[AUTOMOD] Failed to record mod case for %s: %v", p.Author.String(), errCase)
		}
	}

	if db != nil {
		modlog.Log(s, db, &modlog.AutoModViolationEvent{
			GuildID:        p.GuildID,
			TargetUser:     p.Author,
			RuleName:       "Word / Content Filter",
			Action:         p.Punishment,
			ChannelID:      p.ChannelID,
			MatchedKeyword: p.MatchedKeyword,
			Content:        p.Content,
		})
	}
	logger.Infof("[AUTOMOD] Actioned punishment '%s' on %s in Guild %s for reason: %s", p.Punishment, p.Author.String(), p.GuildID, p.Reason)
}

func OnAutoModerationActionExecution(db *database.DB, pol *policy.Module) func(s *discordgo.Session, exec *discordgo.AutoModerationActionExecution) {
	return func(s *discordgo.Session, exec *discordgo.AutoModerationActionExecution) {
		if exec == nil || exec.GuildID == "" || exec.UserID == "" {
			return
		}

		if exec.MessageID != "" {
			MarkAutoModDeleted(exec.MessageID)
		}

		user, err := s.User(exec.UserID)
		if err != nil || user == nil {
			user = &discordgo.User{ID: exec.UserID, Username: "Unknown User"}
		}

		ruleName := "AutoMod Rule"
		if rule, errRule := s.AutoModerationRule(exec.GuildID, exec.RuleID); errRule == nil && rule != nil {
			ruleName = rule.Name
		}

		punishment := "delete"
		reason := "Prohibited message blocked by Discord AutoMod"

		if pol != nil {
			if state, errState := pol.GetFilterState(exec.GuildID); errState == nil && state != nil {
				if ruleName == "Anti-Invite Filter" {
					if state.InvitePunishment != "" {
						punishment = state.InvitePunishment
					}
					reason = "Posted unauthorized Discord invite link"
				} else {
					if state.WordPunishment != "" {
						punishment = state.WordPunishment
					}
					if exec.MatchedKeyword != "" {
						reason = fmt.Sprintf("Used blacklisted word/pattern (%s)", exec.MatchedKeyword)
					} else {
						reason = "Used blacklisted word or regex pattern"
					}
				}
			}
		}

		executePunishment(s, db, punishmentParams{
			GuildID:        exec.GuildID,
			ChannelID:      exec.ChannelID,
			Author:         user,
			Punishment:     punishment,
			Reason:         reason,
			MatchedKeyword: exec.MatchedKeyword,
			Content:        exec.Content,
		})
	}
}
