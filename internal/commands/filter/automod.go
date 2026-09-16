package filter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

const (
	AutoModEventMessageSend   = discordgo.AutoModerationEventMessageSend
	AutoModTriggerTypeKeyword = discordgo.AutoModerationEventTriggerKeyword
	AutoModActionBlockMessage = discordgo.AutoModerationRuleActionBlockMessage

	RuleNameInvite = "Anti-Invite Filter"
	RuleNameWords  = "Word & Regex Filter"
)

var (
	ErrMaxRulesReached = errors.New("server reached maximum limit of 6 AutoMod rules")
	DefaultSyncer      = NewAutoModSyncer(2 * time.Second)
)

type guildSyncTask struct {
	syncInvite bool
	syncWords  bool
	session    *discordgo.Session
	policy     *policy.Module
	timer      *time.Timer
}

type AutoModSyncer struct {
	mu       sync.Mutex
	tasks    map[string]*guildSyncTask
	inFlight map[string]bool
	rerun    map[string]bool
	debounce time.Duration
	wg       sync.WaitGroup
}

func NewAutoModSyncer(debounce time.Duration) *AutoModSyncer {
	return &AutoModSyncer{
		tasks:    make(map[string]*guildSyncTask),
		inFlight: make(map[string]bool),
		rerun:    make(map[string]bool),
		debounce: debounce,
	}
}

func (syncer *AutoModSyncer) QueueSync(s *discordgo.Session, pol *policy.Module, guildID string, syncInvite, syncWords bool) {
	if s == nil || pol == nil || guildID == "" {
		return
	}
	syncer.mu.Lock()
	defer syncer.mu.Unlock()

	task, exists := syncer.tasks[guildID]
	if !exists {
		task = &guildSyncTask{
			session: s,
			policy:  pol,
		}
		syncer.tasks[guildID] = task
	}
	task.syncInvite = task.syncInvite || syncInvite
	task.syncWords = task.syncWords || syncWords
	task.session = s
	task.policy = pol

	if task.timer != nil {
		task.timer.Stop()
	}
	task.timer = time.AfterFunc(syncer.debounce, func() {
		syncer.flushGuild(guildID)
	})
}

func (syncer *AutoModSyncer) flushGuild(guildID string) {
	syncer.mu.Lock()
	task, exists := syncer.tasks[guildID]
	if !exists {
		syncer.mu.Unlock()
		return
	}
	if syncer.inFlight[guildID] {
		syncer.rerun[guildID] = true
		syncer.mu.Unlock()
		return
	}

	delete(syncer.tasks, guildID)
	syncer.inFlight[guildID] = true
	syncInvite := task.syncInvite
	syncWords := task.syncWords
	s := task.session
	pol := task.policy
	syncer.wg.Add(1)
	syncer.mu.Unlock()

	helpers.Spawn(func() {
		defer syncer.wg.Done()
		syncer.executeSync(s, pol, guildID, syncInvite, syncWords)

		syncer.mu.Lock()
		delete(syncer.inFlight, guildID)
		needsRerun := syncer.rerun[guildID]
		delete(syncer.rerun, guildID)
		syncer.mu.Unlock()

		if needsRerun {
			syncer.QueueSync(s, pol, guildID, true, true)
		}
	})
}

func (syncer *AutoModSyncer) executeSync(s *discordgo.Session, pol *policy.Module, guildID string, syncInvite, syncWords bool) {
	state, err := pol.GetFilterState(guildID)
	if err != nil {
		bot.Warnf("[AUTOMOD] Failed to fetch filter state for sync in guild %s: %v", guildID, err)
		return
	}

	if syncInvite {
		if errInv := SyncAutoModInviteRule(s, guildID, state); errInv != nil {
			bot.Warnf("[AUTOMOD] Invite rule sync failed for guild %s: %v", guildID, errInv)
		}
	}
	if syncWords {
		if errWords := SyncAutoModWordRule(s, guildID, state); errWords != nil {
			bot.Warnf("[AUTOMOD] Word rule sync failed for guild %s: %v", guildID, errWords)
		}
	}
}

func (syncer *AutoModSyncer) Flush(ctx context.Context) error {
	syncer.mu.Lock()
	guildIDs := make([]string, 0, len(syncer.tasks))
	for gID, task := range syncer.tasks {
		if task.timer != nil {
			task.timer.Stop()
		}
		guildIDs = append(guildIDs, gID)
	}
	syncer.mu.Unlock()

	for _, gID := range guildIDs {
		syncer.flushGuild(gID)
	}

	done := make(chan struct{})
	helpers.Spawn(func() {
		syncer.wg.Wait()
		close(done)
	})

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isMaxRulesError(err error) bool {
	if err == nil {
		return false
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Message != nil && restErr.Message.Code == 30030 {
		return true
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "30030") || strings.Contains(errMsg, "maximum number of rules")
}

func SyncAutoModInviteRule(s *discordgo.Session, guildID string, state *policy.FilterState) error {
	rules, err := s.AutoModerationRules(guildID)
	if err != nil {
		bot.Warnf("[AUTOMOD] Could not fetch AutoMod rules for Guild %s: %v", guildID, err)
		return fmt.Errorf("failed to fetch AutoMod rules: %w", err)
	}

	var existing *discordgo.AutoModerationRule
	for _, r := range rules {
		if r.Name == RuleNameInvite {
			existing = r
			break
		}
	}

	if state == nil || !state.InviteEnabled {
		if existing != nil {
			if errDel := s.AutoModerationRuleDelete(guildID, existing.ID); errDel != nil {
				bot.Warnf("[AUTOMOD] Failed to delete disabled AutoMod invite rule: %v", errDel)
				return fmt.Errorf("failed to delete AutoMod invite rule: %w", errDel)
			}
		}
		return nil
	}

	keywords := []string{"*discord.gg/*", "*discord.com/invite/*", "*discordapp.com/invite/*"}

	var allowList []string
	if g, errGuild := helpers.GetGuild(s, guildID); errGuild == nil && g != nil && g.VanityURLCode != "" {
		allowList = append(allowList, policy.FormatInviteAllowList(g.VanityURLCode)...)
	}
	for _, inv := range state.AllowedInvites {
		allowList = append(allowList, policy.FormatInviteAllowList(inv)...)
	}

	ruleData := &discordgo.AutoModerationRule{
		Name:        RuleNameInvite,
		EventType:   AutoModEventMessageSend,
		TriggerType: AutoModTriggerTypeKeyword,
		TriggerMetadata: &discordgo.AutoModerationTriggerMetadata{
			KeywordFilter: keywords,
		},
		Actions: []discordgo.AutoModerationAction{
			{
				Type: AutoModActionBlockMessage,
			},
		},
		Enabled: helpers.Ptr(true),
	}

	if len(allowList) > 0 {
		ruleData.TriggerMetadata.AllowList = &allowList
	}
	if len(state.InviteWhitelistedRoles) > 0 {
		ruleData.ExemptRoles = &state.InviteWhitelistedRoles
	}

	if existing != nil {
		_, err = s.AutoModerationRuleEdit(guildID, existing.ID, ruleData)
	} else {
		_, err = s.AutoModerationRuleCreate(guildID, ruleData)
		if isMaxRulesError(err) {
			return ErrMaxRulesReached
		}
	}
	return err
}

func SyncAutoModWordRule(s *discordgo.Session, guildID string, state *policy.FilterState) error {
	rules, err := s.AutoModerationRules(guildID)
	if err != nil {
		bot.Warnf("[AUTOMOD] Could not fetch AutoMod rules for Guild %s: %v", guildID, err)
		return fmt.Errorf("failed to fetch AutoMod rules: %w", err)
	}

	var existing *discordgo.AutoModerationRule
	for _, r := range rules {
		if r.Name == RuleNameWords {
			existing = r
			break
		}
	}

	if state == nil || (len(state.Words) == 0 && len(state.Regexes) == 0) {
		if existing != nil {
			if errDel := s.AutoModerationRuleDelete(guildID, existing.ID); errDel != nil {
				bot.Warnf("[AUTOMOD] Failed to delete empty AutoMod word rule: %v", errDel)
				return fmt.Errorf("failed to delete AutoMod word rule: %w", errDel)
			}
		}
		return nil
	}

	var keywordFilter []string
	for _, w := range state.Words {
		cleaned := strings.TrimSpace(w)
		if cleaned == "" {
			continue
		}
		cleaned = strings.Trim(cleaned, "*")
		if cleaned != "" {
			keywordFilter = append(keywordFilter, "*"+cleaned+"*")
		}
	}

	ruleData := &discordgo.AutoModerationRule{
		Name:        RuleNameWords,
		EventType:   AutoModEventMessageSend,
		TriggerType: AutoModTriggerTypeKeyword,
		TriggerMetadata: &discordgo.AutoModerationTriggerMetadata{
			KeywordFilter: keywordFilter,
			RegexPatterns: state.Regexes,
		},
		Actions: []discordgo.AutoModerationAction{
			{
				Type: AutoModActionBlockMessage,
			},
		},
		Enabled: helpers.Ptr(true),
	}

	if len(state.WordWhitelistedRoles) > 0 {
		ruleData.ExemptRoles = &state.WordWhitelistedRoles
	}

	if existing != nil {
		_, err = s.AutoModerationRuleEdit(guildID, existing.ID, ruleData)
	} else {
		_, err = s.AutoModerationRuleCreate(guildID, ruleData)
		if isMaxRulesError(err) {
			return ErrMaxRulesReached
		}
	}
	return err
}
