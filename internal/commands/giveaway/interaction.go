package giveaway

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobot/config"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/policy/auth"

	"github.com/bwmarrin/discordgo"
)

type giveawayPendingEdit struct {
	timer *time.Timer
}

var (
	giveawayUpdateMu     sync.Mutex
	giveawayPendingEdits = make(map[int64]*giveawayPendingEdit)
)

func queueGiveawayMessageRefresh(s *discordgo.Session, db *database.DB, giveawayID int64, channelID, messageID string) {
	giveawayUpdateMu.Lock()
	defer giveawayUpdateMu.Unlock()

	entry, exists := giveawayPendingEdits[giveawayID]
	if !exists {
		entry = &giveawayPendingEdit{}
		giveawayPendingEdits[giveawayID] = entry
	}

	if entry.timer != nil {
		entry.timer.Stop()
	}

	var myTimer *time.Timer
	myTimer = time.AfterFunc(1500*time.Millisecond, func() {
		giveawayUpdateMu.Lock()
		if cur, ok := giveawayPendingEdits[giveawayID]; ok && cur.timer == myTimer {
			delete(giveawayPendingEdits, giveawayID)
		}
		giveawayUpdateMu.Unlock()

		giveaway, err := db.GetGiveawayByID(giveawayID)
		if err != nil || giveaway == nil || giveaway.Status != "active" {
			return
		}
		count, _ := db.GetGiveawayParticipantCount(giveaway.ID)
		updatedComponents := BuildGiveawayCV2Components(GiveawayDisplayOptions{
			Prize:             giveaway.Prize,
			HostID:            giveaway.CreatedBy,
			WinnersCount:      giveaway.WinnersCount,
			EntriesCount:      count,
			EndTime:           giveaway.EndTime,
			ButtonCustomID:    fmt.Sprintf("giveaway_join_%d", giveaway.ID),
			GiveawayMsgID:     giveaway.MessageID,
			ReqRoleID:         giveaway.ReqRoleID,
			BlacklistedRoleID: giveaway.BlacklistedRoleID,
			MinAccountAgeSec:  giveaway.MinAccountAgeSec,
			MinServerStaySec:  giveaway.MinServerStaySec,
			MinLevel:          giveaway.MinLevel,
		})
		_ = helpers.PatchCV2Message(s, channelID, messageID, updatedComponents, nil)
	})
	entry.timer = myTimer
}

func HandleGiveawayInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, cfg *config.Config) {
	if db == nil {
		return
	}

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		handleSlashCommand(s, i, db, cfg)
	case discordgo.InteractionApplicationCommandAutocomplete:
		handleAutocomplete(s, i, db)
	case discordgo.InteractionMessageComponent:
		handleComponent(s, i, db)
	}
}

func hasGiveawayPermission(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	if i == nil || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return false
	}
	authorizer := auth.NewAuthorizer(auth.NewDiscordStateAdapter(s), nil)
	d := authorizer.Evaluate(context.Background(), auth.Request{
		GuildID:      i.GuildID,
		ChannelID:    i.ChannelID,
		ActorID:      i.Member.User.ID,
		ActorMember:  i.Member,
		RequiredPerm: discordgo.PermissionManageGuild,
	})
	return d.Allowed
}

var durationPresets = []struct {
	name  string
	value string
}{
	{"15 minutes", "15m"},
	{"30 minutes", "30m"},
	{"1 hour", "1h"},
	{"6 hours", "6h"},
	{"12 hours", "12h"},
	{"1 day", "1d"},
	{"2 days", "2d"},
	{"3 days", "3d"},
	{"1 week", "1w"},
}

func buildDurationChoices(input string) []*discordgo.ApplicationCommandOptionChoice {
	input = strings.ToLower(strings.TrimSpace(input))
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(durationPresets))
	for _, p := range durationPresets {
		if input == "" || strings.Contains(p.value, input) || strings.Contains(p.name, input) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: p.name, Value: p.value})
		}
	}
	return choices
}

func buildGiveawayChoices(giveaways []*database.GiveawayRecord, wantActive bool) []*discordgo.ApplicationCommandOptionChoice {
	const maxChoices = 25
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, maxChoices)
	now := time.Now()
	for _, g := range giveaways {
		if g == nil || (g.Status == "active") != wantActive {
			continue
		}
		var label string
		if wantActive {
			label = fmt.Sprintf("%s - ends in %s", g.Prize, helpers.FormatDuration(time.Until(g.EndTime)))
		} else {
			label = fmt.Sprintf("%s - ended %s ago", g.Prize, helpers.FormatDuration(now.Sub(g.EndTime)))
		}
		if runes := []rune(label); len(runes) > 100 {
			label = string(runes[:97]) + "..."
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: label, Value: g.MessageID})
		if len(choices) == maxChoices {
			break
		}
	}
	return choices
}

func focusedOptionName(opts []*discordgo.ApplicationCommandInteractionDataOption) string {
	for _, opt := range opts {
		if opt.Focused {
			return opt.Name
		}
	}
	return ""
}

func handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB) {
	data := i.ApplicationCommandData()
	var choices []*discordgo.ApplicationCommandOptionChoice
	if len(data.Options) == 1 && len(data.Options[0].Options) > 0 {
		subCmd := data.Options[0]
		switch subCmd.Name {
		case "start":
			switch focusedOptionName(subCmd.Options) {
			case "duration", "min_account_age", "min_server_tenure":
				for _, opt := range subCmd.Options {
					if opt.Focused {
						choices = buildDurationChoices(opt.StringValue())
						break
					}
				}
			}
		case "end", "cancel":
			if !hasGiveawayPermission(s, i) {
				break
			}
			if focusedOptionName(subCmd.Options) == "message_id" {
				giveaways, err := db.GetGuildGiveaways(i.GuildID)
				if err == nil {
					choices = buildGiveawayChoices(giveaways, true)
				}
			}
		case "reroll":
			if !hasGiveawayPermission(s, i) {
				break
			}
			if focusedOptionName(subCmd.Options) == "message_id" {
				giveaways, err := db.GetGuildGiveaways(i.GuildID)
				if err == nil {
					choices = buildGiveawayChoices(giveaways, false)
				}
			}
		}
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

func handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, cfg *config.Config) {
	data := i.ApplicationCommandData()
	if data.Name != "giveaway" {
		return
	}

	if !hasGiveawayPermission(s, i) {
		helpers.RespondEphemeral(s, i, "You require the **Manage Server** permission to manage giveaways.")
		return
	}

	if len(data.Options) == 0 {
		helpers.RespondEphemeral(s, i, "select a giveaway subcommand.")
		return
	}

	subCmd := data.Options[0]
	optMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
	for _, opt := range subCmd.Options {
		optMap[opt.Name] = opt
	}

	switch subCmd.Name {
	case "start":
		handleSlashStart(s, i, db, cfg, optMap)
	case "end":
		handleSlashEnd(s, i, db, optMap)
	case "reroll":
		handleSlashReroll(s, i, db, optMap)
	case "list":
		handleSlashList(s, i, db)
	case "cancel":
		handleSlashCancel(s, i, db, optMap)
	}
}

func handleSlashStart(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, cfg *config.Config, optMap map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	prizeOpt, hasPrize := optMap["prize"]
	durOpt, hasDur := optMap["duration"]
	if !hasPrize || !hasDur {
		helpers.RespondEphemeral(s, i, "Prize and duration are required.")
		return
	}

	prize := prizeOpt.StringValue()
	durationStr := durOpt.StringValue()

	duration, err := helpers.ParseDuration(durationStr)
	if err != nil || duration < 5*time.Second {
		helpers.RespondEphemeral(s, i, "provide a valid duration (e.g. `30m`, `2h`, `1d`). Minimum is 5 seconds.")
		return
	}

	maxDur := resolveMaxDuration(cfg)
	if duration > maxDur {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Giveaway duration cannot exceed **%s**.", helpers.FormatDuration(maxDur)))
		return
	}

	winnersCount := 1
	if opt, ok := optMap["winners"]; ok {
		winnersCount = int(opt.IntValue())
	}

	targetChannelID := i.ChannelID
	if opt, ok := optMap["channel"]; ok {
		targetChannelID = opt.ChannelValue(s).ID
	}

	hostedByID := ""
	if i.Member != nil && i.Member.User != nil {
		hostedByID = i.Member.User.ID
	}
	if opt, ok := optMap["host"]; ok {
		if u := opt.UserValue(s); u != nil {
			hostedByID = u.ID
		}
	}

	var reqRoleID string
	if opt, ok := optMap["required_role"]; ok {
		if r := opt.RoleValue(s, i.GuildID); r != nil {
			reqRoleID = r.ID
		}
	}

	var blacklistedRoleID string
	if opt, ok := optMap["blacklisted_role"]; ok {
		if r := opt.RoleValue(s, i.GuildID); r != nil {
			blacklistedRoleID = r.ID
		}
	}

	var minAccountAgeSec int64
	maxReqSec := int64(10 * 365 * 24 * 3600)
	if opt, ok := optMap["min_account_age"]; ok {
		d, errAge := helpers.ParseDuration(opt.StringValue())
		if errAge != nil || d < 0 || int64(d.Seconds()) > maxReqSec {
			helpers.RespondEphemeral(s, i, "Invalid `min_account_age` duration format (e.g. `7d`, `30d`). Maximum is 10 years.")
			return
		}
		minAccountAgeSec = int64(d.Seconds())
	}

	var minServerStaySec int64
	if opt, ok := optMap["min_server_tenure"]; ok {
		d, errStay := helpers.ParseDuration(opt.StringValue())
		if errStay != nil || d < 0 || int64(d.Seconds()) > maxReqSec {
			helpers.RespondEphemeral(s, i, "Invalid `min_server_tenure` duration format (e.g. `24h`, `7d`). Maximum is 10 years.")
			return
		}
		minServerStaySec = int64(d.Seconds())
	}

	var minLevel int
	if opt, ok := optMap["min_level"]; ok {
		minLevel = int(opt.IntValue())
	}

	var actor *discordgo.User
	if i.Member != nil {
		actor = i.Member.User
	}

	_, err = CreateGiveaway(s, db, GiveawayParams{
		GuildID:           i.GuildID,
		ChannelID:         targetChannelID,
		Prize:             prize,
		Duration:          duration,
		WinnersCount:      winnersCount,
		HostedByID:        hostedByID,
		ReqRoleID:         reqRoleID,
		BlacklistedRoleID: blacklistedRoleID,
		MinAccountAgeSec:  minAccountAgeSec,
		MinServerStaySec:  minServerStaySec,
		MinLevel:          minLevel,
		MaxDuration:       maxDur,
		Actor:             actor,
	})
	if err != nil {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Failed to launch giveaway: %v", err))
		return
	}

	helpers.RespondEphemeral(s, i, fmt.Sprintf("Giveaway for **%s** successfully started in <#%s>!", prize, targetChannelID))
}

func handleSlashEnd(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, optMap map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	msgOpt, ok := optMap["message_id"]
	if !ok {
		helpers.RespondEphemeral(s, i, "Please provide the giveaway message ID.")
		return
	}

	msgID := strings.TrimSpace(msgOpt.StringValue())
	g, err := db.GetGiveawayByMessageID(msgID)
	if err != nil || g == nil || g.GuildID != i.GuildID {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Could not find an active giveaway with message ID `%s` in this server.", msgID))
		return
	}

	if g.Status != "active" {
		helpers.RespondEphemeral(s, i, "This giveaway has already ended.")
		return
	}

	var actor *discordgo.User
	if i.Member != nil {
		actor = i.Member.User
	}

	winners, err := EndGiveaway(s, db, g, actor)
	if err != nil {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Failed to end giveaway: %v", err))
		return
	}

	winnerCountText := fmt.Sprintf("**%d** winner(s)", len(winners))
	if len(winners) == 0 {
		winnerCountText = "no participants"
	}
	helpers.RespondEphemeral(s, i, fmt.Sprintf("Successfully ended giveaway for **%s** (%s selected).", g.Prize, winnerCountText))
}

func handleSlashReroll(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, optMap map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	msgOpt, ok := optMap["message_id"]
	if !ok {
		helpers.RespondEphemeral(s, i, "Please provide the giveaway message ID.")
		return
	}

	msgID := strings.TrimSpace(msgOpt.StringValue())
	g, err := db.GetGiveawayByMessageID(msgID)
	if err != nil || g == nil || g.GuildID != i.GuildID {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Could not find a giveaway with message ID `%s` in this server.", msgID))
		return
	}

	var actor *discordgo.User
	if i.Member != nil {
		actor = i.Member.User
	}

	overrideWinners := 0
	if wOpt, ok := optMap["winners"]; ok {
		overrideWinners = int(wOpt.IntValue())
	}

	newWinners, err := RerollGiveaway(s, db, g, actor, overrideWinners)
	if err != nil {
		helpers.RespondEphemeral(s, i, err.Error())
		return
	}

	var mentions []string
	for _, w := range newWinners {
		mentions = append(mentions, fmt.Sprintf("<@%s>", w))
	}
	helpers.RespondEphemeral(s, i, fmt.Sprintf("Rerolled giveaway for **%s**! New winner(s): %s", g.Prize, strings.Join(mentions, ", ")))
}

func handleSlashList(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB) {
	giveaways, err := db.GetGuildGiveaways(i.GuildID)
	if err != nil || len(giveaways) == 0 {
		helpers.RespondEphemeral(s, i, "No giveaways found for this server.")
		return
	}

	const pageSize = 5
	totalPages := (len(giveaways) + pageSize - 1) / pageSize

	components := BuildGiveawayListCV2Components(giveaways, db, 0, totalPages)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral | helpers.CV2FlagsComponentsV2,
			Components: []discordgo.MessageComponent{
				helpers.RawComponent{
					"type":       helpers.ComponentTypeContainer,
					"components": components,
				},
			},
		},
	})
}

func handleSlashCancel(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, optMap map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	msgOpt, ok := optMap["message_id"]
	if !ok {
		helpers.RespondEphemeral(s, i, "Please provide the giveaway message ID.")
		return
	}

	msgID := strings.TrimSpace(msgOpt.StringValue())
	g, err := db.GetGiveawayByMessageID(msgID)
	if err != nil || g == nil || g.GuildID != i.GuildID {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Could not find an active giveaway with message ID `%s` in this server.", msgID))
		return
	}

	if g.Status != "active" {
		helpers.RespondEphemeral(s, i, "Only active giveaways can be cancelled.")
		return
	}

	var actor *discordgo.User
	if i.Member != nil {
		actor = i.Member.User
	}

	if err := CancelGiveaway(s, db, g, actor); err != nil {
		helpers.RespondEphemeral(s, i, fmt.Sprintf("Failed to cancel giveaway: %v", err))
		return
	}

	helpers.RespondEphemeral(s, i, fmt.Sprintf("Successfully cancelled active giveaway for **%s**.", g.Prize))
}

func handleComponent(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB) {
	customID := i.MessageComponentData().CustomID

	switch {
	case strings.HasPrefix(customID, "giveaway_join_"):
		handleJoin(s, i, db, strings.TrimPrefix(customID, "giveaway_join_"))
	case strings.HasPrefix(customID, "giveaway_leave_"):
		handleLeave(s, i, db, strings.TrimPrefix(customID, "giveaway_leave_"))
	case strings.HasPrefix(customID, "glist_prev_"), strings.HasPrefix(customID, "glist_next_"):
		handleListPagination(s, i, db, customID)
	}
}

func handleJoin(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, giveawayIDStr string) {
	gID, err := strconv.ParseInt(giveawayIDStr, 10, 64)
	if err != nil {
		helpers.RespondEphemeral(s, i, "Invalid giveaway ID.")
		return
	}

	giveaway, err := db.GetGiveawayByID(gID)
	if err != nil || giveaway == nil || giveaway.GuildID != i.GuildID || giveaway.Status != "active" {
		helpers.RespondEphemeral(s, i, "This giveaway is no longer active or does not belong to this server.")
		return
	}

	member := i.Member
	userID := ""
	if member != nil && member.User != nil {
		userID = member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if userID == "" {
		return
	}

	if member == nil && i.GuildID != "" {
		fetchedMember, errMem := helpers.GetGuildMember(s, i.GuildID, userID)
		if errMem == nil {
			member = fetchedMember
		}
	}

	if member == nil && (giveaway.ReqRoleID != "" || giveaway.BlacklistedRoleID != "" || giveaway.MinServerStaySec > 0) {
		helpers.RespondEphemeral(s, i, "Unable to verify your member profile and roles. Please try again.")
		return
	}

	if giveaway.ReqRoleID != "" && member != nil {
		hasRole := false
		for _, rID := range member.Roles {
			if rID == giveaway.ReqRoleID {
				hasRole = true
				break
			}
		}
		if !hasRole {
			helpers.RespondEphemeral(s, i, fmt.Sprintf("**Entry Denied**: You must have the <@&%s> role to enter this giveaway.", giveaway.ReqRoleID))
			return
		}
	}

	if giveaway.BlacklistedRoleID != "" && member != nil {
		hasBlacklisted := false
		for _, rID := range member.Roles {
			if rID == giveaway.BlacklistedRoleID {
				hasBlacklisted = true
				break
			}
		}
		if hasBlacklisted {
			helpers.RespondEphemeral(s, i, fmt.Sprintf("**Entry Denied**: Members with the <@&%s> role are not eligible for this giveaway.", giveaway.BlacklistedRoleID))
			return
		}
	}

	if giveaway.MinAccountAgeSec > 0 {
		snowflakeTime, errSnow := discordgo.SnowflakeTimestamp(userID)
		if errSnow != nil {
			helpers.RespondEphemeral(s, i, "Unable to verify your account creation date. Entry rejected.")
			return
		}
		accountAge := time.Since(snowflakeTime)
		minAgeDur := time.Duration(giveaway.MinAccountAgeSec) * time.Second
		if accountAge < minAgeDur {
			helpers.RespondEphemeral(s, i, fmt.Sprintf("**Entry Denied**: Your Discord account must be at least **%s** old to enter (your account age: %s).", helpers.FormatDuration(minAgeDur), helpers.FormatDuration(accountAge)))
			return
		}
	}

	if giveaway.MinServerStaySec > 0 {
		if member == nil || member.JoinedAt.IsZero() {
			helpers.RespondEphemeral(s, i, "Unable to verify your server join date. Entry rejected.")
			return
		}
		serverStay := time.Since(member.JoinedAt)
		minStayDur := time.Duration(giveaway.MinServerStaySec) * time.Second
		if serverStay < minStayDur {
			helpers.RespondEphemeral(s, i, fmt.Sprintf("**Entry Denied**: You must be a member of this server for at least **%s** to enter (your server tenure: %s).", helpers.FormatDuration(minStayDur), helpers.FormatDuration(serverStay)))
			return
		}
	}

	if giveaway.MinLevel > 0 {
		userLevel := 0
		if xpData, errXP := db.GetUserXP(i.GuildID, userID); errXP == nil && xpData != nil {
			userLevel = xpData.Level
		}
		if userLevel < giveaway.MinLevel {
			helpers.RespondEphemeral(s, i, fmt.Sprintf("**Entry Denied**: You must be at least **Level %d** to enter this giveaway (your current level: %d).", giveaway.MinLevel, userLevel))
			return
		}
	}

	joined, err := db.AddGiveawayParticipant(giveaway.ID, userID)
	if err != nil {
		helpers.RespondEphemeral(s, i, "Failed to process giveaway entry.")
		return
	}

	targetMsgID := giveaway.MessageID
	if i.Message != nil && i.Message.ID != "" {
		targetMsgID = i.Message.ID
	}
	queueGiveawayMessageRefresh(s, db, giveaway.ID, i.ChannelID, targetMsgID)

	if !joined {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: discordgo.MessageFlagsEphemeral | helpers.CV2FlagsComponentsV2,
				Components: []discordgo.MessageComponent{
					helpers.RawComponent{
						"type": 17,
						"components": []map[string]interface{}{
							{"type": 10, "content": "You are already entered in this giveaway!"},
							{"type": 14, "divider": true},
							{
								"type": 1,
								"components": []map[string]interface{}{
									{
										"type":      2,
										"style":     4,
										"label":     "Leave Giveaway",
										"custom_id": fmt.Sprintf("giveaway_leave_%d", giveaway.ID),
									},
								},
							},
						},
					},
				},
			},
		})
		return
	}

	helpers.RespondEphemeral(s, i, fmt.Sprintf("🎉 You have entered the giveaway for **%s**!", giveaway.Prize))
}

func handleLeave(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, giveawayIDStr string) {
	gID, err := strconv.ParseInt(giveawayIDStr, 10, 64)
	if err != nil {
		helpers.RespondEphemeral(s, i, "Invalid giveaway ID.")
		return
	}

	giveaway, err := db.GetGiveawayByID(gID)
	if err != nil || giveaway == nil || giveaway.GuildID != i.GuildID || giveaway.Status != "active" {
		helpers.RespondEphemeral(s, i, "This giveaway is no longer active or does not belong to this server.")
		return
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if userID == "" {
		return
	}

	_, err = db.RemoveGiveawayParticipant(giveaway.ID, userID)
	if err != nil {
		helpers.RespondEphemeral(s, i, "Failed to remove you from the giveaway.")
		return
	}

	if i.Message != nil {
		queueGiveawayMessageRefresh(s, db, giveaway.ID, i.ChannelID, i.Message.ID)
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "You have left the giveaway.",
			Flags:      discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{},
		},
	})
}

func handleListPagination(s *discordgo.Session, i *discordgo.InteractionCreate, db *database.DB, customID string) {
	var pageDelta int
	var currentPageStr string

	if strings.HasPrefix(customID, "glist_prev_") {
		pageDelta = -1
		currentPageStr = strings.TrimPrefix(customID, "glist_prev_")
	} else if strings.HasPrefix(customID, "glist_next_") {
		pageDelta = 1
		currentPageStr = strings.TrimPrefix(customID, "glist_next_")
	} else {
		return
	}

	currPage, err := strconv.Atoi(currentPageStr)
	if err != nil {
		return
	}
	newPage := currPage + pageDelta
	if newPage < 0 {
		newPage = 0
	}

	giveaways, err := db.GetGuildGiveaways(i.GuildID)
	if err != nil || len(giveaways) == 0 {
		return
	}

	const pageSize = 5
	totalPages := (len(giveaways) + pageSize - 1) / pageSize
	if newPage >= totalPages {
		newPage = totalPages - 1
	}

	updatedComponents := BuildGiveawayListCV2Components(giveaways, db, newPage, totalPages)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral | helpers.CV2FlagsComponentsV2,
			Components: []discordgo.MessageComponent{
				helpers.RawComponent{
					"type":       helpers.ComponentTypeContainer,
					"components": updatedComponents,
				},
			},
		},
	})
}
