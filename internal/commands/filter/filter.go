package filter

import (
	"errors"
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

const (
	MaxKeywordLength = policy.MaxKeywordLength
	MaxRegexLength   = policy.MaxRegexLength
	MaxRegexCount    = policy.MaxRegexCount
	MaxKeywordCount  = policy.MaxKeywordCount
)

type FilterGroupCmd struct{}

func (c *FilterGroupCmd) Name() string      { return "filter" }
func (c *FilterGroupCmd) Aliases() []string { return []string{"automod", "wordfilter", "invitefilter"} }
func (c *FilterGroupCmd) Category() string  { return "Moderation" }
func (c *FilterGroupCmd) Description() string {
	return "Configures server AutoMod, invite link blocking, and blacklisted word/regex filters."
}
func (c *FilterGroupCmd) Usage() string {
	return "[invites | words | regex] [subcommand / args]"
}
func (c *FilterGroupCmd) Example() string {
	return "invites enable delete | words add badword | regex \\b(bad|word)\\b"
}

func (c *FilterGroupCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "invites", Description: "View anti-invite filter status, whitelisted roles, and allowed invites", Usage: "", Example: ""},
		{Name: "invites enable", Description: "Enable anti-invite filter with punishment", Usage: "[delete|warn|timeout|kick|ban]", Example: "delete"},
		{Name: "invites disable", Description: "Disable anti-invite filter", Usage: "", Example: ""},
		{Name: "invites allow", Description: "Whitelist an invite code or URL", Usage: "<invite_code | link>", Example: "discord.gg/partner"},
		{Name: "invites disallow", Description: "Remove a whitelisted invite code or link", Usage: "<invite_code | link>", Example: "discord.gg/partner"},
		{Name: "invites whitelist", Description: "Whitelist a role from anti-invite filter", Usage: "<@role|roleID>", Example: "@Member"},
		{Name: "invites unwhitelist", Description: "Remove a role from anti-invite whitelist", Usage: "<@role|roleID>", Example: "@Member"},
		{Name: "words", Description: "View blacklisted words list", Usage: "", Example: ""},
		{Name: "words add", Description: "Add one or more words to the blacklist", Usage: "<word1, word2, ...>", Example: "scam, phishing"},
		{Name: "words remove", Description: "Remove a word from the blacklist", Usage: "<word>", Example: "badword"},
		{Name: "words clear", Description: "Clear all blacklisted words", Usage: "", Example: ""},
		{Name: "words whitelist", Description: "Whitelist a role from word filter", Usage: "<@role|roleID>", Example: "@Member"},
		{Name: "words unwhitelist", Description: "Remove a role from word filter whitelist", Usage: "<@role|roleID>", Example: "@Member"},
		{Name: "regex", Description: "View blacklisted regex patterns", Usage: "", Example: ""},
		{Name: "regex add", Description: "Add a regex pattern to the blacklist", Usage: "<pattern>", Example: "\\b(bad|word)\\b"},
		{Name: "regex remove", Description: "Remove a regex pattern from the blacklist", Usage: "<pattern>", Example: "\\b(bad|word)\\b"},
		{Name: "regex clear", Description: "Clear all blacklisted regex patterns", Usage: "", Example: ""},
	}
}

func (c *FilterGroupCmd) Permissions() int64 { return discordgo.PermissionManageGuild }

func (c *FilterGroupCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if ctx.Policy == nil {
		return ctx.SendError("Policy storage is currently unavailable.")
	}
	var invoked string
	if len(ctx.Message.Content) > 0 {
		content := ctx.Message.Content
		if ctx.Prefix != "" && strings.HasPrefix(content, ctx.Prefix) {
			content = content[len(ctx.Prefix):]
		}
		fields := strings.Fields(strings.TrimSpace(content))
		if len(fields) > 0 {
			invoked = strings.ToLower(fields[0])
		}
	}

	var err error
	if invoked == "wordfilter" {
		err = c.handleWordsWithArgs(ctx, ctx.Args)
	} else if invoked == "invitefilter" || invoked == "antilink" || invoked == "antiinvite" {
		err = c.handleInvitesWithArgs(ctx, ctx.Args)
	} else if len(ctx.Args) == 0 {
		err = c.handleStatusOverview(ctx)
	} else {
		subCmd := strings.ToLower(ctx.Args[0])
		switch subCmd {
		case "invites", "invite", "antilink", "antiinvite":
			err = c.handleInvitesWithArgs(ctx, ctx.SubArgs())
		case "words", "word", "blacklist":
			err = c.handleWordsWithArgs(ctx, ctx.SubArgs())
		case "regex", "regexp", "pattern":
			err = c.handleRegexWithArgs(ctx, ctx.SubArgs())
		default:
			err = c.handleStatusOverview(ctx)
		}
	}

	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

func (c *FilterGroupCmd) handleStatusOverview(ctx *bot.Context) error {
	state, err := ctx.Policy.GetFilterState(ctx.Message.GuildID)
	if err != nil {
		bot.Warnf("[POLICY] Failed to fetch filter state for Guild %s: %v", ctx.Message.GuildID, err)
		state = &policy.FilterState{InvitePunishment: "delete", WordPunishment: "delete"}
	}

	inviteStatus := "Disabled"
	if state.InviteEnabled {
		inviteStatus = fmt.Sprintf("Enabled (Punishment: `%s`)", sanitizeCode(state.InvitePunishment))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Server Filter Status",
		Description: "AutoMod safety settings:",
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Anti-Invite Filter", Value: inviteStatus, Inline: false},
			{Name: "Blacklisted Words", Value: fmt.Sprintf("**%d** word(s) configured", len(state.Words)), Inline: true},
			{Name: "Blacklisted Regexes", Value: fmt.Sprintf("**%d** pattern(s) configured", len(state.Regexes)), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use %sfilter invites, %sfilter words, or %sfilter regex for details.", ctx.Prefix, ctx.Prefix, ctx.Prefix),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *FilterGroupCmd) handleInvitesWithArgs(ctx *bot.Context, args []string) error {
	if len(args) == 0 {
		return c.handleInvitesStatus(ctx)
	}

	action := strings.ToLower(args[0])
	switch action {
	case "enable", "on":
		punishment := "delete"
		if len(args) > 1 {
			punishment = strings.ToLower(args[1])
		}
		return c.handleInvitesEnable(ctx, punishment)

	case "disable", "off":
		return c.handleInvitesDisable(ctx)

	case "allow", "allowinvite", "addinvite":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter invites allow <invite_code | link>`", ctx.Prefix)
		}
		return c.handleInvitesAllow(ctx, args[1])

	case "disallow", "removeinvite", "rminvite":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter invites disallow <invite_code | link>`", ctx.Prefix)
		}
		return c.handleInvitesDisallow(ctx, args[1])

	case "whitelist", "role":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter invites whitelist <@role | roleID>`", ctx.Prefix)
		}
		return c.handleRoleWhitelist(ctx, args[1], policy.ExemptionKindInvite, true, "Anti-Invite filter")

	case "unwhitelist", "deny", "removewhitelist":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter invites unwhitelist <@role | roleID>`", ctx.Prefix)
		}
		return c.handleRoleWhitelist(ctx, args[1], policy.ExemptionKindInvite, false, "Anti-Invite whitelist")

	default:
		return c.handleInvitesStatus(ctx)
	}
}

func (c *FilterGroupCmd) handleInvitesStatus(ctx *bot.Context) error {
	state, err := ctx.Policy.GetFilterState(ctx.Message.GuildID)
	if err != nil {
		bot.Warnf("[POLICY] Failed to fetch filter state for Guild %s: %v", ctx.Message.GuildID, err)
		state = &policy.FilterState{InvitePunishment: "delete", WordPunishment: "delete"}
	}

	statusVal := "**Disabled**"
	if state.InviteEnabled {
		statusVal = "**Enabled**"
	}

	var roleMentions []string
	for _, rID := range state.InviteWhitelistedRoles {
		roleMentions = append(roleMentions, fmt.Sprintf("<@&%s>", rID))
	}
	whitelistVal := "None"
	if len(roleMentions) > 0 {
		whitelistVal = strings.Join(roleMentions, ", ")
	}

	allowedInvitesVal := "None"
	if len(state.AllowedInvites) > 0 {
		var formatted []string
		for _, inv := range state.AllowedInvites {
			formatted = append(formatted, fmt.Sprintf("`%s`", sanitizeCode(inv)))
		}
		allowedInvitesVal = strings.Join(formatted, ", ")
	}

	vanityVal := "None"
	if g, errGuild := ctx.Guild(); errGuild == nil && g != nil && g.VanityURLCode != "" {
		vanityVal = fmt.Sprintf("`%s` (automatically whitelisted)", g.VanityURLCode)
	}

	embed := &discordgo.MessageEmbed{
		Title: "Anti-Invite Filter Config",
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Status", Value: statusVal, Inline: true},
			{Name: "Punishment", Value: fmt.Sprintf("`%s`", sanitizeCode(state.InvitePunishment)), Inline: true},
			{Name: "Whitelisted Roles", Value: whitelistVal, Inline: false},
			{Name: "Allowed Partner Invites", Value: allowedInvitesVal, Inline: false},
			{Name: "Server Vanity URL", Value: vanityVal, Inline: false},
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *FilterGroupCmd) handleInvitesAllow(ctx *bot.Context, rawInvite string) error {
	_, err := ctx.Policy.AddAllowedInvite(ctx.Message.GuildID, rawInvite)
	if err != nil {
		if errors.Is(err, policy.ErrAlreadyRestricted) {
			return fmt.Errorf("invite code `%s` is already allowed", sanitizeCode(policy.ExtractInviteCode(rawInvite)))
		}
		return err
	}
	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, true, false)
	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Whitelisted invite `%s`. (AutoMod sync scheduled)", sanitizeCode(policy.ExtractInviteCode(rawInvite)))
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleInvitesDisallow(ctx *bot.Context, rawInvite string) error {
	_, err := ctx.Policy.RemoveAllowedInvite(ctx.Message.GuildID, rawInvite)
	if err != nil {
		if errors.Is(err, policy.ErrNotRestricted) {
			return fmt.Errorf("invite code `%s` is not currently allowed", sanitizeCode(policy.ExtractInviteCode(rawInvite)))
		}
		return err
	}
	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, true, false)
	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Removed invite `%s` from allowed list.", sanitizeCode(policy.ExtractInviteCode(rawInvite)))
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleInvitesEnable(ctx *bot.Context, punishment string) error {
	validPunishments := map[string]bool{"delete": true, "warn": true, "timeout": true, "kick": true, "ban": true}
	if !validPunishments[punishment] {
		punishment = "delete"
	}

	if _, err := ctx.Policy.SetInviteFilter(ctx.Message.GuildID, true, punishment); err != nil {
		bot.Errorf("[POLICY] Failed to update invite filter setting for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to update database for invite filter status")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, true, false)
	_ = ctx.ReactSuccess()

	desc := fmt.Sprintf("Enabled Anti-Invite filter with punishment: `%s`.", sanitizeCode(punishment))
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleInvitesDisable(ctx *bot.Context) error {
	if _, err := ctx.Policy.SetInviteFilter(ctx.Message.GuildID, false, ""); err != nil {
		bot.Errorf("[POLICY] Failed to disable invite filter for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to update database for invite filter status")
	}
	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, true, false)
	_ = ctx.ReactSuccess()

	desc := "Disabled Anti-Invite filter."
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleWordsWithArgs(ctx *bot.Context, args []string) error {
	if len(args) == 0 {
		return c.handleWordsStatus(ctx)
	}

	action := strings.ToLower(args[0])
	switch action {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter words add <word/phrase>`", ctx.Prefix)
		}
		word := policy.CleanInput(strings.Join(args[1:], " "))
		return c.handleWordsAdd(ctx, word)

	case "remove", "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter words remove <word/phrase>`", ctx.Prefix)
		}
		word := policy.CleanInput(strings.Join(args[1:], " "))
		return c.handleWordsRemove(ctx, word)

	case "view", "list", "show":
		return c.handleWordsView(ctx)

	case "clear", "wipe", "reset":
		return c.handleWordsClear(ctx)

	case "whitelist", "allow":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter words whitelist <@role | roleID>`", ctx.Prefix)
		}
		return c.handleRoleWhitelist(ctx, args[1], policy.ExemptionKindWord, true, "word filtering")

	case "unwhitelist", "deny", "removewhitelist":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter words unwhitelist <@role | roleID>`", ctx.Prefix)
		}
		return c.handleRoleWhitelist(ctx, args[1], policy.ExemptionKindWord, false, "word filter whitelist")

	default:
		return c.handleWordsStatus(ctx)
	}
}

func (c *FilterGroupCmd) handleWordsStatus(ctx *bot.Context) error {
	state, err := ctx.Policy.GetFilterState(ctx.Message.GuildID)
	if err != nil {
		bot.Warnf("[POLICY] Failed to fetch filter state for Guild %s: %v", ctx.Message.GuildID, err)
		state = &policy.FilterState{InvitePunishment: "delete", WordPunishment: "delete"}
	}

	var roleMentions []string
	for _, rID := range state.WordWhitelistedRoles {
		roleMentions = append(roleMentions, fmt.Sprintf("<@&%s>", rID))
	}
	whitelistVal := "None"
	if len(roleMentions) > 0 {
		whitelistVal = strings.Join(roleMentions, ", ")
	}

	embed := &discordgo.MessageEmbed{
		Title: "Word Filter Config",
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Blacklisted Count", Value: fmt.Sprintf("**%d** word(s)", len(state.Words)), Inline: true},
			{Name: "Punishment", Value: fmt.Sprintf("`%s`", sanitizeCode(state.WordPunishment)), Inline: true},
			{Name: "Whitelisted Roles", Value: whitelistVal, Inline: false},
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *FilterGroupCmd) handleWordsAdd(ctx *bot.Context, rawInput string) error {
	rawTokens := strings.Split(rawInput, ",")
	tokens := make([]string, 0, len(rawTokens))
	for _, t := range rawTokens {
		cleaned := policy.CleanInput(t)
		if cleaned != "" {
			tokens = append(tokens, cleaned)
		}
	}
	if len(tokens) == 0 {
		return fmt.Errorf("blacklisted word cannot be empty")
	}

	if len(tokens) == 1 {
		word := tokens[0]
		_, err := ctx.Policy.AddWord(ctx.Message.GuildID, word)
		if err != nil {
			if errors.Is(err, policy.ErrWordRequired) {
				return fmt.Errorf("blacklisted word cannot be empty")
			}
			if errors.Is(err, policy.ErrWordTooLong) {
				return fmt.Errorf("word exceeds maximum length of %d characters", MaxKeywordLength)
			}
			if errors.Is(err, policy.ErrAlreadyRestricted) {
				return fmt.Errorf("`%s` is already in the blacklist", sanitizeCode(word))
			}
			if errors.Is(err, policy.ErrLimitReached) {
				return fmt.Errorf("guild blacklist word limit of %d reached", MaxKeywordCount)
			}
			bot.Errorf("[POLICY] Failed to add blacklisted word '%s' for Guild %s: %v", word, ctx.Message.GuildID, err)
			return fmt.Errorf("failed to save blacklisted word")
		}

		DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

		_ = ctx.ReactSuccess()
		desc := fmt.Sprintf("Added `%s` to server blacklisted words.", sanitizeCode(word))
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
		return err
	}

	_, added, err := ctx.Policy.AddWords(ctx.Message.GuildID, tokens)
	if err != nil {
		bot.Errorf("[POLICY] Failed bulk addition for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to save blacklisted words")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Added %d words to server blacklist. (Duplicates or over-length words skipped)", added)
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleWordsRemove(ctx *bot.Context, word string) error {
	_, err := ctx.Policy.RemoveWord(ctx.Message.GuildID, word)
	if err != nil {
		if errors.Is(err, policy.ErrWordRequired) {
			return fmt.Errorf("specify a word to remove")
		}
		if errors.Is(err, policy.ErrNotRestricted) {
			return fmt.Errorf("`%s` is not found in the blacklisted words list", sanitizeCode(word))
		}
		bot.Errorf("[POLICY] Failed to remove blacklisted word '%s' for Guild %s: %v", word, ctx.Message.GuildID, err)
		return fmt.Errorf("failed to remove blacklisted word")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Removed `%s` from blacklisted words.", sanitizeCode(word))
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleWordsView(ctx *bot.Context) error {
	state, err := ctx.Policy.GetFilterState(ctx.Message.GuildID)
	if err != nil {
		bot.Errorf("[POLICY] Failed to fetch filter state for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to fetch blacklisted words")
	}

	if len(state.Words) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: "No blacklisted words currently configured for this server.",
		})
		return err
	}

	var pages []string
	var currentChunk []string
	currentLen := 0

	for _, w := range state.Words {
		cleaned := sanitizeCode(w)
		itemLen := len([]rune(cleaned)) + 4
		if currentLen+itemLen > 1800 && len(currentChunk) > 0 {
			pages = append(pages, fmt.Sprintf("`%s`", strings.Join(currentChunk, "`, `")))
			currentChunk = nil
			currentLen = 0
		}
		currentChunk = append(currentChunk, cleaned)
		currentLen += itemLen
	}
	if len(currentChunk) > 0 {
		pages = append(pages, fmt.Sprintf("`%s`", strings.Join(currentChunk, "`, `")))
	}

	var embeds []*discordgo.MessageEmbed
	for i, page := range pages {
		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Blacklisted Words (%d)", len(state.Words)),
			Description: page,
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Page %d/%d", i+1, len(pages)),
			},
		})
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func (c *FilterGroupCmd) handleWordsClear(ctx *bot.Context) error {
	confirmed, err := ctx.PromptConfirmation("Are you sure you want to permanently clear all blacklisted words for this server?")
	if err != nil || !confirmed {
		return err
	}

	_, err = ctx.Policy.ClearWords(ctx.Message.GuildID)
	if err != nil {
		bot.Errorf("[POLICY] Failed to clear blacklisted words for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to clear blacklisted words")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := "Wiped all blacklisted words from server list."
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleRegexWithArgs(ctx *bot.Context, args []string) error {
	if len(args) == 0 {
		return c.handleRegexList(ctx)
	}

	action := strings.ToLower(args[0])
	switch action {
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter regex add <pattern>`", ctx.Prefix)
		}
		pattern := policy.CleanInput(strings.Join(args[1:], " "))
		return c.handleRegexAdd(ctx, pattern)

	case "remove", "delete", "rm", "del":
		if len(args) < 2 {
			return fmt.Errorf("usage: `%sfilter regex remove <pattern>`", ctx.Prefix)
		}
		pattern := policy.CleanInput(strings.Join(args[1:], " "))
		return c.handleRegexRemove(ctx, pattern)

	case "list", "view", "show":
		return c.handleRegexList(ctx)

	case "clear", "reset":
		return c.handleRegexClear(ctx)

	default:
		pattern := policy.CleanInput(strings.Join(args, " "))
		if strings.ContainsAny(pattern, `\^$.*+?()[]{}|`) {
			return c.handleRegexAdd(ctx, pattern)
		}
		return fmt.Errorf("invalid subcommand. Usage: `%sfilter regex [add | remove | list | clear] <pattern>`", ctx.Prefix)
	}
}

func (c *FilterGroupCmd) handleRegexAdd(ctx *bot.Context, pattern string) error {
	_, err := ctx.Policy.AddRegex(ctx.Message.GuildID, pattern)
	if err != nil {
		if errors.Is(err, policy.ErrRegexRequired) {
			return fmt.Errorf("regex pattern cannot be empty")
		}
		if errors.Is(err, policy.ErrRegexTooLong) {
			return fmt.Errorf("pattern exceeds maximum Discord AutoMod length of %d characters", MaxRegexLength)
		}
		if errors.Is(err, policy.ErrRegexInvalid) {
			var regexErr *policy.RegexError
			if errors.As(err, &regexErr) {
				return fmt.Errorf("invalid regular expression syntax: `%s`", sanitizeCode(regexErr.Error()))
			}
			return fmt.Errorf("invalid regular expression syntax")
		}
		if errors.Is(err, policy.ErrAlreadyRestricted) {
			return fmt.Errorf("this regex already exists in the blacklist")
		}
		if errors.Is(err, policy.ErrLimitReached) {
			return fmt.Errorf("discord automod limit reached (%d regex patterns max)", MaxRegexCount)
		}
		bot.Errorf("[POLICY] Failed to add blacklisted regex pattern '%s' for Guild %s: %v", pattern, ctx.Message.GuildID, err)
		return fmt.Errorf("failed to save regex pattern to database")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Added regex pattern `%s` to server filter.", sanitizeCode(pattern))
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleRegexRemove(ctx *bot.Context, pattern string) error {
	_, err := ctx.Policy.RemoveRegex(ctx.Message.GuildID, pattern)
	if err != nil {
		if errors.Is(err, policy.ErrRegexRequired) {
			return fmt.Errorf("specify a regex pattern to remove")
		}
		if errors.Is(err, policy.ErrNotRestricted) {
			return fmt.Errorf("the regex pattern was not found in the blacklist")
		}
		bot.Errorf("[POLICY] Failed to remove blacklisted regex pattern '%s' for Guild %s: %v", pattern, ctx.Message.GuildID, err)
		return fmt.Errorf("failed to remove regex pattern from database")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := fmt.Sprintf("Removed regex pattern `%s` from server filter.", sanitizeCode(pattern))
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleRegexList(ctx *bot.Context) error {
	state, err := ctx.Policy.GetFilterState(ctx.Message.GuildID)
	if err != nil {
		bot.Errorf("[POLICY] Failed to fetch filter state for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to fetch blacklisted regexes")
	}

	if len(state.Regexes) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: "No blacklisted regex patterns configured for this server.",
		})
		return err
	}

	var formatted []string
	for i, r := range state.Regexes {
		formatted = append(formatted, fmt.Sprintf("%d. `%s`", i+1, sanitizeCode(r)))
	}

	pageSize := 10
	totalPages := (len(formatted) + pageSize - 1) / pageSize
	var embeds []*discordgo.MessageEmbed

	for i := 0; i < len(formatted); i += pageSize {
		end := i + pageSize
		if end > len(formatted) {
			end = len(formatted)
		}
		pageIndex := (i / pageSize) + 1
		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Blacklisted Regex Patterns (%d)", len(state.Regexes)),
			Description: strings.Join(formatted[i:end], "\n"),
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Page %d/%d", pageIndex, totalPages),
			},
		})
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func (c *FilterGroupCmd) handleRegexClear(ctx *bot.Context) error {
	confirmed, err := ctx.PromptConfirmation("Are you sure you want to permanently clear all blacklisted regex patterns for this server?")
	if err != nil || !confirmed {
		return err
	}

	_, err = ctx.Policy.ClearRegexes(ctx.Message.GuildID)
	if err != nil {
		bot.Errorf("[POLICY] Failed to clear blacklisted regexes for Guild %s: %v", ctx.Message.GuildID, err)
		return fmt.Errorf("failed to clear regex patterns from database")
	}

	DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)

	_ = ctx.ReactSuccess()
	desc := "Cleared all blacklisted regex patterns."
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func (c *FilterGroupCmd) handleRoleWhitelist(ctx *bot.Context, target string, kind policy.ExemptionKind, isAdd bool, filterLabel string) error {
	role, err := ctx.ResolveRole(target)
	if err != nil || role == nil {
		actionVerb := "whitelist"
		if !isAdd {
			actionVerb = "remove"
		}
		return fmt.Errorf("specify a valid role to %s", actionVerb)
	}

	if isAdd {
		_, err := ctx.Policy.AddExemption(ctx.Message.GuildID, kind, role.ID)
		if err != nil {
			if errors.Is(err, policy.ErrAlreadyExempt) {
				return fmt.Errorf("<@&%s> is already whitelisted", role.ID)
			}
			bot.Errorf("[POLICY] Failed to add exemption for role %s in Guild %s: %v", role.ID, ctx.Message.GuildID, err)
			return fmt.Errorf("failed to update database records")
		}
	} else {
		_, err := ctx.Policy.RemoveExemption(ctx.Message.GuildID, kind, role.ID)
		if err != nil {
			if errors.Is(err, policy.ErrNotExempt) {
				return fmt.Errorf("<@&%s> is not currently whitelisted", role.ID)
			}
			bot.Errorf("[POLICY] Failed to remove exemption for role %s in Guild %s: %v", role.ID, ctx.Message.GuildID, err)
			return fmt.Errorf("failed to update database records")
		}
	}

	if kind == policy.ExemptionKindInvite {
		DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, true, false)
	} else if kind == policy.ExemptionKindWord {
		DefaultSyncer.QueueSync(ctx.Session, ctx.Policy, ctx.Message.GuildID, false, true)
	}

	_ = ctx.ReactSuccess()

	var desc string
	if isAdd {
		desc = fmt.Sprintf("Whitelisted role <@&%s> from %s.", role.ID, filterLabel)
	} else {
		desc = fmt.Sprintf("Removed role <@&%s> from %s.", role.ID, filterLabel)
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: desc})
	return err
}

func sanitizeCode(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}
