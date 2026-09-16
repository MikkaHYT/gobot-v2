package giveaway

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

var (
	activeTimersMu   sync.Mutex
	activeTimers     = make(map[int64]*time.Timer)
	rerollMu         sync.Mutex
	rerollInFlight   = make(map[int64]struct{})
	schedulerWG      sync.WaitGroup
	schedulerStopped bool
)

func cryptoShuffle(slice []string) error {
	for i := len(slice) - 1; i > 0; i-- {
		nBig, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return fmt.Errorf("random num generation fail: %w", err)
		}
		j := int(nBig.Int64())
		slice[i], slice[j] = slice[j], slice[i]
	}
	return nil
}

func ScheduleGiveaway(s *discordgo.Session, db *database.DB, g *database.GiveawayRecord) {
	if g == nil || g.Status != "active" {
		return
	}

	activeTimersMu.Lock()
	if schedulerStopped {
		activeTimersMu.Unlock()
		return
	}
	if existing, ok := activeTimers[g.ID]; ok {
		existing.Stop()
	}

	wait := time.Until(g.EndTime)
	if wait <= 0 {
		schedulerWG.Add(1)
		activeTimersMu.Unlock()
		helpers.Spawn(func() {
			defer schedulerWG.Done()
			if _, errEnd := EndGiveaway(s, db, g, nil); errEnd != nil {
				logger.Warnf("[GIVEAWAY] Failed to auto-end giveaway %d: %v", g.ID, errEnd)
			}
		})
		return
	}

	gID := g.ID
	timer := time.AfterFunc(wait, func() {
		activeTimersMu.Lock()
		if schedulerStopped {
			activeTimersMu.Unlock()
			return
		}
		delete(activeTimers, gID)
		freshG, err := db.GetGiveawayByID(gID)
		if err == nil && freshG != nil && freshG.Status == "active" {
			schedulerWG.Add(1)
			activeTimersMu.Unlock()
			helpers.Spawn(func() {
				defer schedulerWG.Done()
				if _, errEnd := EndGiveaway(s, db, freshG, nil); errEnd != nil {
					logger.Warnf("[GIVEAWAY] Failed to auto-end giveaway %d: %v", freshG.ID, errEnd)
				}
			})
			return
		}
		activeTimersMu.Unlock()
	})

	activeTimers[gID] = timer
	activeTimersMu.Unlock()
}

func isParticipantEligible(db *database.DB, g *database.GiveawayRecord, m *discordgo.Member) bool {
	if m == nil || m.User == nil || m.User.Bot {
		return false
	}

	if g.MinAccountAgeSec > 0 {
		createdAt, errSnow := discordgo.SnowflakeTimestamp(m.User.ID)
		if errSnow != nil || time.Since(createdAt) < time.Duration(g.MinAccountAgeSec)*time.Second {
			return false
		}
	}
	if g.ReqRoleID != "" {
		hasRole := false
		for _, rID := range m.Roles {
			if rID == g.ReqRoleID {
				hasRole = true
				break
			}
		}
		if !hasRole {
			return false
		}
	}

	if g.BlacklistedRoleID != "" {
		for _, rID := range m.Roles {
			if rID == g.BlacklistedRoleID {
				return false
			}
		}
	}

	if g.MinServerStaySec > 0 {
		if m.JoinedAt.IsZero() || time.Since(m.JoinedAt) < time.Duration(g.MinServerStaySec)*time.Second {
			return false
		}
	}

	if g.MinLevel > 0 && db != nil {
		xpData, err := db.GetUserXP(g.GuildID, m.User.ID)
		if err != nil {
			bot.Warnf("[GIVEAWAY] Failed to query user XP during eligibility check (User %s Guild %s): %v", m.User.ID, g.GuildID, err)
		} else if xpData != nil && xpData.Level < g.MinLevel {
			return false
		}
	}

	return true
}

func StopGiveawayScheduler() {
	activeTimersMu.Lock()
	schedulerStopped = true
	for id, timer := range activeTimers {
		if timer != nil {
			timer.Stop()
		}
		delete(activeTimers, id)
	}
	activeTimersMu.Unlock()

	schedulerWG.Wait()
}

func InitGiveawayScheduler(s *discordgo.Session, db *database.DB) {
	if db == nil || s == nil {
		return
	}

	activeTimersMu.Lock()
	schedulerStopped = false
	activeTimersMu.Unlock()

	activeGiveaways, err := db.GetActiveGiveaways()
	if err != nil {
		bot.Errorf("[GIVEAWAY] Failed to fetch active giveaways for scheduler: %v", err)
		return
	}

	now := time.Now()
	endedCount := 0
	scheduledCount := 0

	for _, g := range activeGiveaways {
		if !g.EndTime.After(now) {
			endedCount++
			schedulerWG.Add(1)
			rec := g
			helpers.Spawn(func() {
				defer schedulerWG.Done()
				if _, errEnd := EndGiveaway(s, db, rec, nil); errEnd != nil {
					logger.Warnf("[GIVEAWAY] Failed to auto-end overdue giveaway %d: %v", rec.ID, errEnd)
				}
			})
		} else {
			scheduledCount++
			ScheduleGiveaway(s, db, g)
		}
	}

	if endedCount > 0 || scheduledCount > 0 {
		bot.Infof("[GIVEAWAY] Scheduler initialized: %d overdue giveaways auto-ended, %d scheduled.", endedCount, scheduledCount)
	}
}

func EndGiveaway(s *discordgo.Session, db *database.DB, g *database.GiveawayRecord, actor *discordgo.User) ([]string, error) {
	if s == nil || db == nil || g == nil {
		return nil, fmt.Errorf("invalid arguments")
	}

	participants, err := db.GetGiveawayParticipants(g.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch participants: %w", err)
	}

	actualEndTime := time.Now()
	g.EndTime = actualEndTime

	var activeParticipants []string
	for _, pID := range participants {
		member, _ := helpers.GetGuildMember(s, g.GuildID, pID)

		if isParticipantEligible(db, g, member) {
			activeParticipants = append(activeParticipants, pID)
		}
	}

	var winners []string
	winnersText := "No valid participants"

	if len(activeParticipants) > 0 {
		winnersCount := g.WinnersCount
		if winnersCount > len(activeParticipants) {
			winnersCount = len(activeParticipants)
		}

		if errShuffle := cryptoShuffle(activeParticipants); errShuffle != nil {
			return nil, errShuffle
		}
		for i := 0; i < winnersCount; i++ {
			winners = append(winners, activeParticipants[i])
		}

		var mentions []string
		for _, w := range winners {
			mentions = append(mentions, fmt.Sprintf("<@%s>", w))
		}
		winnersText = strings.Join(mentions, ", ")
	}

	updated, errEnd := db.EndGiveawayAtomic(g.ID, winners, actualEndTime)
	if errEnd != nil {
		return nil, errEnd
	}
	if !updated {
		return nil, fmt.Errorf("giveaway already finalized")
	}

	activeTimersMu.Lock()
	if t, ok := activeTimers[g.ID]; ok {
		t.Stop()
		delete(activeTimers, g.ID)
	}
	activeTimersMu.Unlock()

	endedComponents := BuildEndedGiveawayCV2Components(EndedGiveawayDisplayOptions{
		Prize:         g.Prize,
		HostID:        g.CreatedBy,
		WinnersText:   winnersText,
		EntriesCount:  len(participants),
		EndTime:       actualEndTime,
		GiveawayMsgID: g.MessageID,
	})
	_ = helpers.PatchCV2Message(s, g.ChannelID, g.MessageID, endedComponents, nil)

	if len(winners) > 0 {
		congratMsg := fmt.Sprintf("Congratulations %s! You won **%s**!", winnersText, g.Prize)
		msgRef := &discordgo.MessageReference{
			MessageID: g.MessageID,
			ChannelID: g.ChannelID,
			GuildID:   g.GuildID,
		}
		_, _ = s.ChannelMessageSendReply(g.ChannelID, congratMsg, msgRef)
	}

	modlog.Log(s, db, &modlog.GiveawayEvent{
		GuildID:     g.GuildID,
		Action:      "Ended",
		Prize:       g.Prize,
		ChannelID:   g.ChannelID,
		MessageID:   g.MessageID,
		HostID:      g.CreatedBy,
		WinnersText: winnersText,
		Actor:       actor,
	})

	return winners, nil
}

func RerollGiveaway(s *discordgo.Session, db *database.DB, g *database.GiveawayRecord, actor *discordgo.User, overrideWinners int) ([]string, error) {
	if s == nil || db == nil || g == nil {
		return nil, fmt.Errorf("invalid arguments")
	}
	if g.Status != "ended" {
		return nil, fmt.Errorf("giveaway is not ended yet")
	}
	rerollMu.Lock()
	if _, exists := rerollInFlight[g.ID]; exists {
		rerollMu.Unlock()
		return nil, fmt.Errorf("giveaway reroll already in progress")
	}
	rerollInFlight[g.ID] = struct{}{}
	rerollMu.Unlock()
	defer func() {
		rerollMu.Lock()
		delete(rerollInFlight, g.ID)
		rerollMu.Unlock()
	}()

	participants, err := db.GetGiveawayParticipants(g.ID)
	if err != nil || len(participants) == 0 {
		return nil, fmt.Errorf("no participants to reroll")
	}

	var activeParticipants []string
	for _, pID := range participants {
		member, _ := helpers.GetGuildMember(s, g.GuildID, pID)

		if isParticipantEligible(db, g, member) {
			activeParticipants = append(activeParticipants, pID)
		}
	}

	if len(activeParticipants) == 0 {
		return nil, fmt.Errorf("no active participants found in the server to reroll")
	}

	winnersCount := g.WinnersCount
	if overrideWinners > 0 {
		winnersCount = overrideWinners
	}
	if winnersCount > len(activeParticipants) {
		winnersCount = len(activeParticipants)
	}

	if errShuffle := cryptoShuffle(activeParticipants); errShuffle != nil {
		return nil, errShuffle
	}
	var newWinners []string
	for i := 0; i < winnersCount; i++ {
		newWinners = append(newWinners, activeParticipants[i])
	}

	if err := db.UpdateGiveawayStatusAndWinners(g.ID, "ended", newWinners, g.EndTime); err != nil {
		return nil, fmt.Errorf("failed to update rerolled winners in database: %w", err)
	}

	var mentions []string
	for _, w := range newWinners {
		mentions = append(mentions, fmt.Sprintf("<@%s>", w))
	}
	winnersText := strings.Join(mentions, ", ")

	actorMention := "A mod"
	if actor != nil {
		actorMention = fmt.Sprintf("<@%s>", actor.ID)
	}

	rerollMsg := fmt.Sprintf("%s rerolled the giveaway! Congratulations %s! You won **%s**!", actorMention, winnersText, g.Prize)
	msgRef := &discordgo.MessageReference{
		MessageID: g.MessageID,
		ChannelID: g.ChannelID,
		GuildID:   g.GuildID,
	}

	_, _ = s.ChannelMessageSendReply(g.ChannelID, rerollMsg, msgRef)

	endedComponents := BuildEndedGiveawayCV2Components(EndedGiveawayDisplayOptions{
		Prize:         g.Prize,
		HostID:        g.CreatedBy,
		WinnersText:   winnersText,
		EntriesCount:  len(participants),
		EndTime:       g.EndTime,
		GiveawayMsgID: g.MessageID,
	})
	_ = helpers.PatchCV2Message(s, g.ChannelID, g.MessageID, endedComponents, nil)

	modlog.Log(s, db, &modlog.GiveawayEvent{
		GuildID:     g.GuildID,
		Action:      "Rerolled",
		Prize:       g.Prize,
		ChannelID:   g.ChannelID,
		MessageID:   g.MessageID,
		HostID:      g.CreatedBy,
		WinnersText: winnersText,
		Actor:       actor,
	})

	return newWinners, nil
}

func CancelGiveaway(s *discordgo.Session, db *database.DB, g *database.GiveawayRecord, actor *discordgo.User) error {
	if g == nil || g.Status != "active" {
		return fmt.Errorf("giveaway is not active")
	}

	activeTimersMu.Lock()
	if timer, ok := activeTimers[g.ID]; ok {
		timer.Stop()
		delete(activeTimers, g.ID)
	}
	activeTimersMu.Unlock()

	changed, err := db.CancelGiveawayAtomic(g.ID)
	if err != nil {
		return fmt.Errorf("failed to cancel giveaway in database: %w", err)
	}
	if !changed {
		return fmt.Errorf("giveaway already ended")
	}

	_ = s.ChannelMessageDelete(g.ChannelID, g.MessageID)
	modlog.Log(s, db, &modlog.GiveawayEvent{
		GuildID:   g.GuildID,
		Action:    "Cancelled",
		Prize:     g.Prize,
		ChannelID: g.ChannelID,
		MessageID: g.MessageID,
		HostID:    g.CreatedBy,
		Actor:     actor,
	})
	return nil
}
