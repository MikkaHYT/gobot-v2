package giveaway

import (
	"fmt"
	"strings"
	"time"

	"gobot/config"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type GiveawayDisplayOptions struct {
	Prize             string
	HostID            string
	WinnersCount      int
	EntriesCount      int
	EndTime           time.Time
	ButtonCustomID    string
	GiveawayMsgID     string
	ReqRoleID         string
	BlacklistedRoleID string
	MinAccountAgeSec  int64
	MinServerStaySec  int64
	MinLevel          int
}

func BuildGiveawayCV2Components(opts GiveawayDisplayOptions) []map[string]interface{} {
	titleText := fmt.Sprintf("## %s - Giveaway:", opts.Prize)
	bodyText := fmt.Sprintf("> **Hosted By:** <@%s>\n> **Winners:** %d\n> **Entries:** %d\n> **Ends:** <t:%d:R> (<t:%d:f>)", opts.HostID, opts.WinnersCount, opts.EntriesCount, opts.EndTime.Unix(), opts.EndTime.Unix())

	var reqList []string
	if opts.ReqRoleID != "" {
		reqList = append(reqList, fmt.Sprintf("• **Required Role:** <@&%s>", opts.ReqRoleID))
	}
	if opts.BlacklistedRoleID != "" {
		reqList = append(reqList, fmt.Sprintf("• **Blacklisted Role:** <@&%s>", opts.BlacklistedRoleID))
	}
	if opts.MinAccountAgeSec > 0 {
		reqList = append(reqList, fmt.Sprintf("• **Min Account Age:** `%s`", helpers.FormatDuration(time.Duration(opts.MinAccountAgeSec)*time.Second)))
	}
	if opts.MinServerStaySec > 0 {
		reqList = append(reqList, fmt.Sprintf("• **Min Member Time:** `%s`", helpers.FormatDuration(time.Duration(opts.MinServerStaySec)*time.Second)))
	}
	if opts.MinLevel > 0 {
		reqList = append(reqList, fmt.Sprintf("• **Min Level:** `Level %d`", opts.MinLevel))
	}

	if len(reqList) > 0 {
		bodyText += "\n\n**Requirements:**\n> " + strings.Join(reqList, "\n> ")
	}

	buttonMap := map[string]interface{}{
		"type":      2,
		"style":     1,
		"label":     "Enter Giveaway",
		"custom_id": opts.ButtonCustomID,
	}

	footerText := fmt.Sprintf("-# Giveaway ID: %s", opts.GiveawayMsgID)

	return []map[string]interface{}{
		{"type": 10, "content": titleText},
		{"type": 10, "content": bodyText},
		{"type": 14, "divider": true},
		{
			"type": 1,
			"components": []map[string]interface{}{
				buttonMap,
			},
		},
		{"type": 14, "divider": true},
		{"type": 10, "content": footerText},
	}
}

type EndedGiveawayDisplayOptions struct {
	Prize         string
	HostID        string
	WinnersText   string
	EntriesCount  int
	EndTime       time.Time
	GiveawayMsgID string
}

func BuildEndedGiveawayCV2Components(opts EndedGiveawayDisplayOptions) []map[string]interface{} {
	titleText := fmt.Sprintf("### %s - Giveaway Ended:", opts.Prize)
	bodyText := fmt.Sprintf("> **Hosted By:** <@%s>\n> **Winner(s):** %s\n> **Entries:** %d\n> **Ended:** <t:%d:R> (<t:%d:f>)", opts.HostID, opts.WinnersText, opts.EntriesCount, opts.EndTime.Unix(), opts.EndTime.Unix())

	buttonMap := map[string]interface{}{
		"type":      2,
		"style":     2,
		"label":     "Giveaway Ended",
		"custom_id": fmt.Sprintf("giveaway_ended_%s", opts.GiveawayMsgID),
		"disabled":  true,
	}

	footerText := fmt.Sprintf("-# Giveaway ID: %s", opts.GiveawayMsgID)

	return []map[string]interface{}{
		{"type": 10, "content": titleText},
		{"type": 10, "content": bodyText},
		{"type": 14, "divider": true},
		{
			"type": 1,
			"components": []map[string]interface{}{
				buttonMap,
			},
		},
		{"type": 14, "divider": true},
		{"type": 10, "content": footerText},
	}
}

func BuildGiveawayListCV2Components(giveaways []*database.GiveawayRecord, db *database.DB, currentPage int, totalPages int) []map[string]interface{} {
	const pageSize = 5
	startIdx := currentPage * pageSize
	endIdx := startIdx + pageSize
	if endIdx > len(giveaways) {
		endIdx = len(giveaways)
	}

	var result []map[string]interface{}

	result = append(result, map[string]interface{}{
		"type":    10,
		"content": "### Server Giveaways",
	})
	result = append(result, map[string]interface{}{
		"type":    10,
		"content": fmt.Sprintf("Page **%d** of **%d** • Total Giveaways: `%d`", currentPage+1, totalPages, len(giveaways)),
	})
	result = append(result, map[string]interface{}{
		"type":    14,
		"divider": true,
	})

	for idx := startIdx; idx < endIdx; idx++ {
		g := giveaways[idx]
		partCount, _ := db.GetGiveawayParticipantCount(g.ID)

		statusStr := fmt.Sprintf("Active (Ends <t:%d:R>)", g.EndTime.Unix())
		if g.Status == "ended" {
			statusStr = fmt.Sprintf("Ended (<t:%d:R>)", g.EndTime.Unix())
		}

		itemText := fmt.Sprintf("**%d. %s**\n> **Status:** %s\n> **Hosted By:** <@%s>\n> **Winners Count:** %d\n> **Entries:** %d\n> **Channel:** <#%s>\n> **Message ID:** `%s`",
			idx+1, g.Prize, statusStr, g.CreatedBy, g.WinnersCount, partCount, g.ChannelID, g.MessageID)

		result = append(result, map[string]interface{}{
			"type":    10,
			"content": itemText,
		})
	}

	if totalPages > 1 {
		result = append(result, map[string]interface{}{"type": 14, "divider": true})
		result = append(result, map[string]interface{}{
			"type": 1,
			"components": []map[string]interface{}{
				{
					"type":      2,
					"style":     2,
					"label":     "Previous",
					"custom_id": fmt.Sprintf("glist_prev_%d", currentPage),
					"disabled":  currentPage == 0,
				},
				{
					"type":      2,
					"style":     2,
					"label":     "Next",
					"custom_id": fmt.Sprintf("glist_next_%d", currentPage),
					"disabled":  currentPage == totalPages-1,
				},
			},
		})
	}

	return result
}

type GiveawayParams struct {
	GuildID           string
	ChannelID         string
	Prize             string
	Duration          time.Duration
	WinnersCount      int
	HostedByID        string
	ReqRoleID         string
	BlacklistedRoleID string
	MinAccountAgeSec  int64
	MinServerStaySec  int64
	MinLevel          int
	MaxDuration       time.Duration
	Actor             *discordgo.User
}

func resolveMaxDuration(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.MaxGiveawayDuration > 0 {
		return cfg.MaxGiveawayDuration
	}
	return config.DefaultConfig().MaxGiveawayDuration
}

func ValidateGiveawayParams(params GiveawayParams, maxDuration time.Duration) error {
	if params.GuildID == "" || params.ChannelID == "" {
		return fmt.Errorf("guild ID and channel ID are required")
	}
	if strings.TrimSpace(params.Prize) == "" {
		return fmt.Errorf("giveaway prize cannot be empty")
	}
	if params.Duration < 5*time.Second {
		return fmt.Errorf("giveaway duration must be at least 5 seconds")
	}
	effectiveMax := params.MaxDuration
	if effectiveMax <= 0 {
		effectiveMax = maxDuration
	}
	if effectiveMax <= 0 {
		effectiveMax = config.DefaultConfig().MaxGiveawayDuration
	}
	if params.Duration > effectiveMax {
		return fmt.Errorf("giveaway duration cannot exceed %s", helpers.FormatDuration(effectiveMax))
	}
	if params.WinnersCount <= 0 || params.WinnersCount > 50 {
		return fmt.Errorf("number of winners must be between 1 and 50 (provided: %d)", params.WinnersCount)
	}
	if params.ReqRoleID != "" && params.BlacklistedRoleID != "" && params.ReqRoleID == params.BlacklistedRoleID {
		return fmt.Errorf("required role and blacklisted role cannot be the same")
	}
	if params.MinAccountAgeSec < 0 {
		return fmt.Errorf("minimum account age cannot be negative")
	}
	if params.MinServerStaySec < 0 {
		return fmt.Errorf("minimum membership time cannot be negative")
	}
	if params.MinLevel < 0 {
		return fmt.Errorf("minimum level cannot be negative")
	}
	return nil
}

func CreateGiveaway(s *discordgo.Session, db *database.DB, params GiveawayParams) (*database.GiveawayRecord, error) {
	if err := ValidateGiveawayParams(params, params.MaxDuration); err != nil {
		return nil, err
	}

	if ch, errCh := s.Channel(params.ChannelID); errCh == nil && ch != nil {
		if ch.GuildID != "" && ch.GuildID != params.GuildID {
			return nil, fmt.Errorf("target channel does not belong to this server")
		}
	}

	if params.HostedByID == "" && params.Actor != nil {
		params.HostedByID = params.Actor.ID
	}

	startTime := time.Now().UTC()
	endTime := startTime.Add(params.Duration)

	tempComponents := BuildGiveawayCV2Components(GiveawayDisplayOptions{
		Prize:             params.Prize,
		HostID:            params.HostedByID,
		WinnersCount:      params.WinnersCount,
		EntriesCount:      0,
		EndTime:           endTime,
		ButtonCustomID:    "giveaway_join_pending",
		GiveawayMsgID:     "Pending...",
		ReqRoleID:         params.ReqRoleID,
		BlacklistedRoleID: params.BlacklistedRoleID,
		MinAccountAgeSec:  params.MinAccountAgeSec,
		MinServerStaySec:  params.MinServerStaySec,
		MinLevel:          params.MinLevel,
	})

	msg, err := helpers.SendCV2MessageAndReturn(s, params.ChannelID, tempComponents, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to send giveaway message: %w", err)
	}

	gRecord, err := db.CreateGiveaway(database.CreateGiveawayParams{
		GuildID:           params.GuildID,
		ChannelID:         params.ChannelID,
		MessageID:         msg.ID,
		Prize:             params.Prize,
		WinnersCount:      params.WinnersCount,
		CreatedBy:         params.HostedByID,
		StartTime:         startTime,
		EndTime:           endTime,
		ReqRoleID:         params.ReqRoleID,
		BlacklistedRoleID: params.BlacklistedRoleID,
		MinAccountAgeSec:  params.MinAccountAgeSec,
		MinServerStaySec:  params.MinServerStaySec,
		MinLevel:          params.MinLevel,
	})
	if err != nil {
		_ = s.ChannelMessageDelete(params.ChannelID, msg.ID)
		return nil, fmt.Errorf("failed to save giveaway to database: %w", err)
	}

	finalComponents := BuildGiveawayCV2Components(GiveawayDisplayOptions{
		Prize:             params.Prize,
		HostID:            params.HostedByID,
		WinnersCount:      params.WinnersCount,
		EntriesCount:      0,
		EndTime:           endTime,
		ButtonCustomID:    fmt.Sprintf("giveaway_join_%d", gRecord.ID),
		GiveawayMsgID:     msg.ID,
		ReqRoleID:         params.ReqRoleID,
		BlacklistedRoleID: params.BlacklistedRoleID,
		MinAccountAgeSec:  params.MinAccountAgeSec,
		MinServerStaySec:  params.MinServerStaySec,
		MinLevel:          params.MinLevel,
	})

	if errPatch := helpers.PatchCV2Message(s, params.ChannelID, msg.ID, finalComponents, nil); errPatch != nil {
		_ = s.ChannelMessageDelete(params.ChannelID, msg.ID)
		if _, errCancel := db.CancelGiveawayAtomic(gRecord.ID); errCancel != nil {
			logger.Warnf("[GIVEAWAY] Failed to cancel giveaway %d during rollback: %v", gRecord.ID, errCancel)
		}
		return nil, fmt.Errorf("failed to finalize giveaway message components: %w", errPatch)
	}

	ScheduleGiveaway(s, db, gRecord)
	modlog.Log(s, db, &modlog.GiveawayEvent{
		GuildID:   params.GuildID,
		Action:    "Started",
		Prize:     params.Prize,
		ChannelID: params.ChannelID,
		MessageID: msg.ID,
		HostID:    params.HostedByID,
		Actor:     params.Actor,
	})

	return gRecord, nil
}
