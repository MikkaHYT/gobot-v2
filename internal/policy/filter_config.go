package policy

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxKeywordLength = 60
	MaxRegexLength   = 260
	MaxKeywordCount  = 1000
	MaxRegexCount    = 10
)

type ExemptionKind string

const (
	ExemptionKindInvite ExemptionKind = "invite"
	ExemptionKindWord   ExemptionKind = "word"
)

type FilterState struct {
	InviteEnabled          bool
	InvitePunishment       string
	InviteWhitelistedRoles []string
	AllowedInvites         []string
	WordWhitelistedRoles   []string
	WordPunishment         string
	Words                  []string
	Regexes                []string
}

func CleanInput(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u200B' || r == '\uFEFF' || r == '\u00AD'
	})
}

func (m *Module) GetFilterState(guildID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	cfg, err := m.db.GetFilterConfig(guildID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, ErrPolicyUnavailable
	}
	words, err := m.db.GetBlacklistedWords(guildID)
	if err != nil {
		return nil, err
	}
	regexes, err := m.db.GetBlacklistedRegexes(guildID)
	if err != nil {
		return nil, err
	}
	allowedInvites, err := m.db.GetAllowedInvites(guildID)
	if err != nil {
		return nil, err
	}

	punish := cfg.InvitePunishment
	if punish == "" {
		punish = "delete"
	}
	wordPunish := cfg.WordPunishment
	if wordPunish == "" {
		wordPunish = "delete"
	}

	return &FilterState{
		InviteEnabled:          cfg.InviteEnabled,
		InvitePunishment:       punish,
		InviteWhitelistedRoles: slices.Clone(cfg.InviteWhitelistedRoles),
		AllowedInvites:         slices.Clone(allowedInvites),
		WordWhitelistedRoles:   slices.Clone(cfg.WordWhitelistedRoles),
		WordPunishment:         wordPunish,
		Words:                  slices.Clone(words),
		Regexes:                slices.Clone(regexes),
	}, nil
}

func (m *Module) SetInviteFilter(guildID string, enabled bool, punishment string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	p := strings.ToLower(strings.TrimSpace(punishment))
	switch p {
	case "delete", "timeout", "kick", "ban", "none", "":
	default:
		p = "delete"
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if err := m.db.SetInviteFilter(guildID, enabled, p); err != nil {
		return nil, err
	}
	state.InviteEnabled = enabled
	if p != "" {
		state.InvitePunishment = p
	}
	return state, nil
}

func (m *Module) AddWord(guildID, rawWord string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	word := strings.ToLower(CleanInput(rawWord))
	if word == "" {
		return nil, ErrWordRequired
	}
	if utf8.RuneCountInString(word) > MaxKeywordLength {
		return nil, ErrWordTooLong
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}

	for _, w := range state.Words {
		if strings.EqualFold(w, word) {
			return nil, ErrAlreadyRestricted
		}
	}
	if len(state.Words) >= MaxKeywordCount {
		return nil, ErrLimitReached
	}

	if err := m.db.AddBlacklistedWord(guildID, word); err != nil {
		return nil, err
	}
	state.Words = append(state.Words, word)
	return state, nil
}

func (m *Module) RemoveWord(guildID, rawWord string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	word := strings.ToLower(CleanInput(rawWord))
	if word == "" {
		return nil, ErrWordRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}

	matchedWord := ""
	for _, w := range state.Words {
		if strings.EqualFold(w, word) {
			matchedWord = w
			break
		}
	}
	if matchedWord == "" {
		return nil, ErrNotRestricted
	}

	if err := m.db.RemoveBlacklistedWord(guildID, matchedWord); err != nil {
		return nil, err
	}
	remaining := state.Words[:0]
	for _, w := range state.Words {
		if w != matchedWord {
			remaining = append(remaining, w)
		}
	}
	state.Words = remaining
	return state, nil
}

func (m *Module) ClearWords(guildID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if err := m.db.ClearBlacklistedWords(guildID); err != nil {
		return nil, err
	}
	state.Words = []string{}
	return state, nil
}

func (m *Module) AddRegex(guildID, rawPattern string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	pattern := CleanInput(rawPattern)
	if pattern == "" {
		return nil, ErrRegexRequired
	}
	if utf8.RuneCountInString(pattern) > MaxRegexLength {
		return nil, ErrRegexTooLong
	}

	if _, err := regexp.Compile(pattern); err != nil {
		return nil, &RegexError{err: err}
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}

	for _, p := range state.Regexes {
		if p == pattern {
			return nil, ErrAlreadyRestricted
		}
	}
	if len(state.Regexes) >= MaxRegexCount {
		return nil, ErrLimitReached
	}

	if err := m.db.AddBlacklistedRegex(guildID, pattern); err != nil {
		return nil, err
	}
	state.Regexes = append(state.Regexes, pattern)
	return state, nil
}

func (m *Module) RemoveRegex(guildID, rawPattern string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	pattern := CleanInput(rawPattern)
	if pattern == "" {
		return nil, ErrRegexRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}

	matchedPattern := ""
	for _, p := range state.Regexes {
		if p == pattern {
			matchedPattern = p
			break
		}
	}
	if matchedPattern == "" {
		return nil, ErrNotRestricted
	}

	if err := m.db.RemoveBlacklistedRegex(guildID, matchedPattern); err != nil {
		return nil, err
	}
	remaining := state.Regexes[:0]
	for _, p := range state.Regexes {
		if p != matchedPattern {
			remaining = append(remaining, p)
		}
	}
	state.Regexes = remaining
	return state, nil
}

func (m *Module) ClearRegexes(guildID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if err := m.db.ClearBlacklistedRegexes(guildID); err != nil {
		return nil, err
	}
	state.Regexes = []string{}
	return state, nil
}

func (m *Module) AddExemption(guildID string, kind ExemptionKind, roleID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return nil, ErrTargetRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if kind == ExemptionKindInvite {
		if err := m.db.AddInviteFilterWhitelistedRole(guildID, roleID); err != nil {
			return nil, err
		}
		state.InviteWhitelistedRoles = append(state.InviteWhitelistedRoles, roleID)
	} else if kind == ExemptionKindWord {
		if err := m.db.AddWordFilterWhitelistedRole(guildID, roleID); err != nil {
			return nil, err
		}
		state.WordWhitelistedRoles = append(state.WordWhitelistedRoles, roleID)
	} else {
		return nil, ErrUnsupportedAction
	}

	return state, nil
}

func (m *Module) RemoveExemption(guildID string, kind ExemptionKind, roleID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return nil, ErrTargetRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if kind == ExemptionKindInvite {
		if err := m.db.RemoveInviteFilterWhitelistedRole(guildID, roleID); err != nil {
			return nil, err
		}
		state.InviteWhitelistedRoles = removeRole(state.InviteWhitelistedRoles, roleID)
	} else if kind == ExemptionKindWord {
		if err := m.db.RemoveWordFilterWhitelistedRole(guildID, roleID); err != nil {
			return nil, err
		}
		state.WordWhitelistedRoles = removeRole(state.WordWhitelistedRoles, roleID)
	} else {
		return nil, ErrUnsupportedAction
	}

	return state, nil
}

func removeRole(roles []string, roleID string) []string {
	remaining := roles[:0]
	for _, role := range roles {
		if role != roleID {
			remaining = append(remaining, role)
		}
	}
	return remaining
}

func ExtractInviteCode(input string) string {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "www.")
	s = strings.TrimPrefix(s, "discord.gg/")
	s = strings.TrimPrefix(s, "discord.com/invite/")
	s = strings.TrimPrefix(s, "discordapp.com/invite/")
	s = strings.Trim(s, "* /")
	if idx := strings.IndexAny(s, "?#/"); idx != -1 {
		s = s[:idx]
	}
	return strings.ToLower(s)
}

func FormatInviteAllowList(code string) []string {
	clean := ExtractInviteCode(code)
	if clean == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("*discord.gg/%s*", clean),
		fmt.Sprintf("*discord.com/invite/%s*", clean),
		fmt.Sprintf("*discordapp.com/invite/%s*", clean),
	}
}

func (m *Module) AddWords(guildID string, rawWords []string) (*FilterState, int, error) {
	if m == nil || m.db == nil {
		return nil, 0, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, 0, ErrGuildRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, 0, err
	}

	existingMap := make(map[string]bool, len(state.Words))
	for _, w := range state.Words {
		existingMap[strings.ToLower(w)] = true
	}

	validWords := make([]string, 0, len(rawWords))
	for _, raw := range rawWords {
		w := strings.ToLower(CleanInput(raw))
		if w == "" || existingMap[w] {
			continue
		}
		if utf8.RuneCountInString(w) > MaxKeywordLength {
			continue
		}
		if len(existingMap)+len(validWords) >= MaxKeywordCount {
			break
		}
		existingMap[w] = true
		validWords = append(validWords, w)
	}

	if len(validWords) == 0 {
		return state, 0, nil
	}

	added, err := m.db.AddBlacklistedWords(guildID, validWords)
	if err != nil {
		return nil, 0, err
	}
	state.Words = append(state.Words, validWords...)
	return state, added, nil
}

func (m *Module) AddAllowedInvite(guildID, rawInvite string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	code := ExtractInviteCode(rawInvite)
	if code == "" {
		return nil, errors.New("invalid invite code or link")
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	for _, existing := range state.AllowedInvites {
		if existing == code {
			return nil, ErrAlreadyRestricted
		}
	}

	if err := m.db.AddAllowedInvite(guildID, code); err != nil {
		return nil, err
	}
	state.AllowedInvites = append(state.AllowedInvites, code)
	return state, nil
}

func (m *Module) RemoveAllowedInvite(guildID, rawInvite string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	code := ExtractInviteCode(rawInvite)
	if code == "" {
		return nil, errors.New("invalid invite code or link")
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}

	found := false
	for _, existing := range state.AllowedInvites {
		if existing == code {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotRestricted
	}

	if err := m.db.RemoveAllowedInvite(guildID, code); err != nil {
		return nil, err
	}
	newInvites := make([]string, 0, len(state.AllowedInvites))
	for _, item := range state.AllowedInvites {
		if item != code {
			newInvites = append(newInvites, item)
		}
	}
	state.AllowedInvites = newInvites
	return state, nil
}

func (m *Module) ClearAllowedInvites(guildID string) (*FilterState, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	state, err := m.GetFilterState(guildID)
	if err != nil {
		return nil, err
	}
	if err := m.db.ClearAllowedInvites(guildID); err != nil {
		return nil, err
	}
	state.AllowedInvites = nil
	return state, nil
}
