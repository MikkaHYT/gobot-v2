package utility

import (
	"fmt"
	"sort"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type ServerinfoCmd struct{}

func (c *ServerinfoCmd) Name() string        { return "serverinfo" }
func (c *ServerinfoCmd) Aliases() []string   { return []string{"server", "guildinfo", "gi"} }
func (c *ServerinfoCmd) Category() string    { return "Utility" }
func (c *ServerinfoCmd) Description() string { return "Displays server statistics and overview." }
func (c *ServerinfoCmd) Usage() string       { return "[--detailed|-d]" }
func (c *ServerinfoCmd) Example() string     { return "--detailed" }

func (c *ServerinfoCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	detailed := false
	for _, arg := range ctx.Args {
		if strings.EqualFold(arg, "--detailed") || strings.EqualFold(arg, "-d") || strings.EqualFold(arg, "full") {
			detailed = true
			break
		}
	}

	embed, err := buildServerEmbed(ctx, detailed)
	if err != nil {
		return fmt.Errorf("failed to build serverinfo: %w", err)
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type DetailedServerinfoCmd struct{}

func (c *DetailedServerinfoCmd) Name() string { return "detailedserverinfo" }
func (c *DetailedServerinfoCmd) Aliases() []string {
	return []string{"dsi", "detailedsi", "fullserverinfo"}
}
func (c *DetailedServerinfoCmd) Category() string { return "Utility" }
func (c *DetailedServerinfoCmd) Description() string {
	return "Displays detailed information about the server."
}
func (c *DetailedServerinfoCmd) Usage() string   { return "" }
func (c *DetailedServerinfoCmd) Example() string { return "" }

func (c *DetailedServerinfoCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	embed, err := buildServerEmbed(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to build detailed serverinfo: %w", err)
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func buildServerEmbed(ctx *bot.Context, detailed bool) (*discordgo.MessageEmbed, error) {
	guildID := ctx.Message.GuildID
	guild, err := fetchGuildWithFallbacks(ctx, guildID)
	if err != nil {
		return nil, err
	}

	owner := formatServerOwner(ctx, guild.OwnerID)
	created := formatServerCreated(guild.ID)
	humans, bots, totalMembers := countGuildMembers(ctx, guild)

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    guild.Name,
			IconURL: guild.IconURL("4096"),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: guild.IconURL("4096"),
		},
		Color: helpers.ColorDefault,
	}
	applyServerMediaEmbed(guild, embed)

	embed.Fields = append(embed.Fields, buildServerOverviewField(guild, owner, created, detailed))
	embed.Fields = append(embed.Fields, buildServerMembersField(guild, humans, bots, totalMembers, detailed))

	if detailed {
		embed.Fields = append(embed.Fields, buildServerChannelsField(guild))
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Boost Status",
			Value:  fmt.Sprintf("• **Tier:** %s\n• **Total Boosts:** `%s`", formatPremiumTier(guild.PremiumTier), helpers.FormatNumber(guild.PremiumSubscriptionCount)),
			Inline: false,
		})
		embed.Fields = append(embed.Fields, buildServerSecurityField(ctx, guild))
		embed.Fields = append(embed.Fields, buildServerAssetsField(guild))
		if sysField := buildServerSystemChannelsField(guild); sysField != nil {
			embed.Fields = append(embed.Fields, sysField)
		}
		if intField := buildServerIntegrationsField(ctx, guild); intField != nil {
			embed.Fields = append(embed.Fields, intField)
		}
		if len(guild.Features) > 0 {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("Server Features [%d]", len(guild.Features)),
				Value:  formatFeatures(guild.Features),
				Inline: false,
			})
		}
	} else {
		statsVal := fmt.Sprintf(
			"• **Boosts:** `%s` (%s)\n• **Roles:** `%s` | **Emojis:** `%s`\n• **Stickers:** `%s`",
			helpers.FormatNumber(guild.PremiumSubscriptionCount),
			formatPremiumTier(guild.PremiumTier),
			helpers.FormatNumber(len(guild.Roles)),
			helpers.FormatNumber(len(guild.Emojis)),
			helpers.FormatNumber(len(guild.Stickers)),
		)
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Server Stats",
			Value:  statsVal,
			Inline: true,
		})
	}

	if mediaField := buildServerMediaLinksField(guild, detailed); mediaField != nil {
		embed.Fields = append(embed.Fields, mediaField)
	}

	return embed, nil
}

func fetchGuildWithFallbacks(ctx *bot.Context, guildID string) (*discordgo.Guild, error) {
	guild, err := ctx.Session.GuildWithCounts(guildID)
	if err != nil {
		guild, err = ctx.Session.Guild(guildID)
		if err != nil {
			return nil, fmt.Errorf("fetching guild data: %w", err)
		}
	}

	if len(guild.Channels) == 0 {
		if cached, _ := ctx.Session.State.Guild(guildID); cached != nil && len(cached.Channels) > 0 {
			guild.Channels = cached.Channels
		} else if channels, err := ctx.Session.GuildChannels(guildID); err == nil {
			guild.Channels = channels
		}
	}
	if len(guild.Roles) == 0 {
		if cached, _ := ctx.Session.State.Guild(guildID); cached != nil && len(cached.Roles) > 0 {
			guild.Roles = cached.Roles
		} else if roles, err := ctx.Session.GuildRoles(guildID); err == nil {
			guild.Roles = roles
		}
	}
	if len(guild.Emojis) == 0 {
		if cached, _ := ctx.Session.State.Guild(guildID); cached != nil && len(cached.Emojis) > 0 {
			guild.Emojis = cached.Emojis
		} else if emojis, err := ctx.Session.GuildEmojis(guildID); err == nil {
			guild.Emojis = emojis
		}
	}
	return guild, nil
}

func formatServerOwner(ctx *bot.Context, ownerID string) string {
	owner := fmt.Sprintf("<@%s>", ownerID)
	if ownerUser, err := ctx.Session.User(ownerID); err == nil && ownerUser != nil {
		owner = fmt.Sprintf("%s (`%s`)", ownerUser.String(), ownerID)
	}
	return owner
}

func formatServerCreated(guildID string) string {
	if ts, err := discordgo.SnowflakeTimestamp(guildID); err == nil {
		return fmt.Sprintf("<t:%d:F> (<t:%d:R>)", ts.Unix(), ts.Unix())
	}
	return "Unknown"
}

func countGuildMembers(ctx *bot.Context, guild *discordgo.Guild) (humans, bots, totalMembers int) {
	totalMembers = guild.ApproximateMemberCount
	if totalMembers == 0 {
		totalMembers = guild.MemberCount
	}

	members := guild.Members
	if len(members) == 0 && ctx.Session.State != nil {
		if cached, err := ctx.Session.State.Guild(guild.ID); err == nil && cached != nil {
			members = cached.Members
		}
	}

	if (len(members) == 0 || (totalMembers > 0 && len(members) < totalMembers)) && totalMembers <= 3000 {
		targetCount := totalMembers
		if targetCount <= 0 {
			targetCount = 1000
		}
		if fetched, err := helpers.FetchGuildMembers(ctx.Session, guild.ID, targetCount); err == nil && len(fetched) > len(members) {
			members = fetched
		}
	}

	for _, m := range members {
		if m == nil || m.User == nil {
			continue
		}
		if m.User.Bot {
			bots++
		} else {
			humans++
		}
	}

	if totalMembers == 0 {
		totalMembers = len(members)
	}
	return humans, bots, totalMembers
}

func applyServerMediaEmbed(guild *discordgo.Guild, embed *discordgo.MessageEmbed) {
	if guild.Banner != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: guild.BannerURL("4096")}
	} else if guild.Splash != "" {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: fmt.Sprintf("https://cdn.discordapp.com/splashes/%s/%s.png?size=4096", guild.ID, guild.Splash),
		}
	}
}

func buildServerOverviewField(guild *discordgo.Guild, owner, created string, detailed bool) *discordgo.MessageEmbedField {
	var overview strings.Builder
	if detailed {
		overview.WriteString(fmt.Sprintf("• **Server Name:** `%s`\n", guild.Name))
		overview.WriteString(fmt.Sprintf("• **Server ID:** `%s`\n", guild.ID))
		overview.WriteString(fmt.Sprintf("• **Owner:** %s\n", owner))
		overview.WriteString(fmt.Sprintf("• **Created:** %s\n", created))
		overview.WriteString(fmt.Sprintf("• **Preferred Locale:** `%s`", guild.PreferredLocale))
	} else {
		overview.WriteString(fmt.Sprintf("• **Owner:** %s\n", owner))
		overview.WriteString(fmt.Sprintf("• **Created:** %s\n", created))
		overview.WriteString(fmt.Sprintf("• **Server ID:** `%s`", guild.ID))
	}

	if guild.VanityURLCode != "" {
		overview.WriteString(fmt.Sprintf("\n• **Vanity URL:** `discord.gg/%s`", guild.VanityURLCode))
	}
	if detailed && guild.Description != "" {
		overview.WriteString(fmt.Sprintf("\n• **Description:** *\"%s\"*", guild.Description))
	}

	return &discordgo.MessageEmbedField{
		Name:   "Server Overview",
		Value:  overview.String(),
		Inline: false,
	}
}

func buildServerMembersField(guild *discordgo.Guild, humans, bots, totalMembers int, detailed bool) *discordgo.MessageEmbedField {
	var memberInfo strings.Builder
	memberInfo.WriteString(fmt.Sprintf("• **Total Members:** `%s`", helpers.FormatNumber(totalMembers)))

	counted := humans + bots
	hasAccurateBreakdown := counted > 0 && (totalMembers == 0 || counted >= totalMembers || (totalMembers > 0 && float64(counted)/float64(totalMembers) >= 0.95))
	if hasAccurateBreakdown {
		memberInfo.WriteString(fmt.Sprintf("\n• **Humans:** `%s` | **Bots:** `%s`", helpers.FormatNumber(humans), helpers.FormatNumber(bots)))
	}
	if guild.ApproximatePresenceCount > 0 {
		memberInfo.WriteString(fmt.Sprintf("\n• **Online:** `%s`", helpers.FormatNumber(guild.ApproximatePresenceCount)))
	}
	if detailed && guild.MaxMembers > 0 {
		memberInfo.WriteString(fmt.Sprintf("\n• **Max Capacity:** `%s`", helpers.FormatNumber(guild.MaxMembers)))
	}

	return &discordgo.MessageEmbedField{
		Name:   "Members",
		Value:  memberInfo.String(),
		Inline: !detailed,
	}
}

func buildServerChannelsField(guild *discordgo.Guild) *discordgo.MessageEmbedField {
	var textChans, voiceChans, catChans, newsChans, stageChans, forumChans, mediaChans, threads int
	for _, ch := range guild.Channels {
		switch ch.Type {
		case discordgo.ChannelTypeGuildText:
			textChans++
		case discordgo.ChannelTypeGuildVoice:
			voiceChans++
		case discordgo.ChannelTypeGuildCategory:
			catChans++
		case discordgo.ChannelTypeGuildNews:
			newsChans++
		case discordgo.ChannelTypeGuildStageVoice:
			stageChans++
		case discordgo.ChannelTypeGuildForum:
			forumChans++
		case discordgo.ChannelTypeGuildMedia:
			mediaChans++
		case discordgo.ChannelTypeGuildPublicThread, discordgo.ChannelTypeGuildPrivateThread:
			threads++
		}
	}

	chanSummary := fmt.Sprintf(
		"• **Total Channels:** `%s` (Categories: `%s`)\n• **Text:** `%s` | **Voice:** `%s` | **Forum:** `%s`\n• **News:** `%s` | **Stage:** `%s` | **Media:** `%s`",
		helpers.FormatNumber(len(guild.Channels)), helpers.FormatNumber(catChans), helpers.FormatNumber(textChans), helpers.FormatNumber(voiceChans), helpers.FormatNumber(forumChans), helpers.FormatNumber(newsChans), helpers.FormatNumber(stageChans), helpers.FormatNumber(mediaChans),
	)
	if threads > 0 {
		chanSummary += fmt.Sprintf("\n• **Active Threads:** `%s`", helpers.FormatNumber(threads))
	}

	return &discordgo.MessageEmbedField{
		Name:   "Channels",
		Value:  chanSummary,
		Inline: false,
	}
}

func buildServerSecurityField(ctx *bot.Context, guild *discordgo.Guild) *discordgo.MessageEmbedField {
	secSummary := fmt.Sprintf(
		"• **Verification Level:** `%s`\n• **Explicit Content Filter:** `%s`\n• **Default Notifications:** `%s`\n• **2FA / MFA Requirement:** `%s`\n• **NSFW Level:** `%s`",
		formatVerificationLevel(guild.VerificationLevel),
		formatExplicitFilter(guild.ExplicitContentFilter),
		formatNotificationLevel(guild.DefaultMessageNotifications),
		formatMFALevel(guild.MfaLevel),
		formatNSFWLevel(guild.NSFWLevel),
	)

	if automod, err := ctx.Session.AutoModerationRules(guild.ID); err == nil && len(automod) > 0 {
		secSummary += fmt.Sprintf("\n• **AutoMod Rules:** `%s active`", helpers.FormatNumber(len(automod)))
	}

	return &discordgo.MessageEmbedField{
		Name:   "Security & Moderation",
		Value:  secSummary,
		Inline: false,
	}
}

func buildServerAssetsField(guild *discordgo.Guild) *discordgo.MessageEmbedField {
	var staticEmojis, animEmojis, managedEmojis int
	for _, e := range guild.Emojis {
		if e.Managed {
			managedEmojis++
		}
		if e.Animated {
			animEmojis++
		} else {
			staticEmojis++
		}
	}

	var hoistedRoles, managedRoles, mentionableRoles int
	var highestRole *discordgo.Role

	if len(guild.Roles) > 0 {
		sortedRoles := make([]*discordgo.Role, len(guild.Roles))
		copy(sortedRoles, guild.Roles)
		sort.Slice(sortedRoles, func(i, j int) bool {
			return sortedRoles[i].Position > sortedRoles[j].Position
		})
		highestRole = sortedRoles[0]

		for _, r := range guild.Roles {
			if r.Hoist {
				hoistedRoles++
			}
			if r.Managed {
				managedRoles++
			}
			if r.Mentionable {
				mentionableRoles++
			}
		}
	}

	highestRoleStr := "None"
	if highestRole != nil {
		highestRoleStr = fmt.Sprintf("<@&%s>", highestRole.ID)
	}

	assetSummary := fmt.Sprintf(
		"• **Roles:** `%s` (Highest: %s)\n  *Hoisted:* `%s` | *Managed/Bot:* `%s` | *Mentionable:* `%s`\n• **Emojis:** `%s` (Static: `%s` | Animated: `%s` | Managed: `%s`)\n• **Stickers:** `%s`",
		helpers.FormatNumber(len(guild.Roles)), highestRoleStr, helpers.FormatNumber(hoistedRoles), helpers.FormatNumber(managedRoles), helpers.FormatNumber(mentionableRoles),
		helpers.FormatNumber(len(guild.Emojis)), helpers.FormatNumber(staticEmojis), helpers.FormatNumber(animEmojis), helpers.FormatNumber(managedEmojis),
		helpers.FormatNumber(len(guild.Stickers)),
	)

	return &discordgo.MessageEmbedField{
		Name:   "Roles & Custom Assets",
		Value:  assetSummary,
		Inline: false,
	}
}

func buildServerSystemChannelsField(guild *discordgo.Guild) *discordgo.MessageEmbedField {
	var sysChans []string
	if guild.SystemChannelID != "" {
		sysChans = append(sysChans, fmt.Sprintf("• **System:** <#%s>", guild.SystemChannelID))
	}
	if guild.RulesChannelID != "" {
		sysChans = append(sysChans, fmt.Sprintf("• **Rules:** <#%s>", guild.RulesChannelID))
	}
	if guild.PublicUpdatesChannelID != "" {
		sysChans = append(sysChans, fmt.Sprintf("• **Updates:** <#%s>", guild.PublicUpdatesChannelID))
	}
	if guild.AfkChannelID != "" {
		sysChans = append(sysChans, fmt.Sprintf("• **AFK:** <#%s> (`%ds timeout`)", guild.AfkChannelID, guild.AfkTimeout))
	}

	if len(sysChans) == 0 {
		return nil
	}

	sysSummary := strings.Join(sysChans, "\n")
	if guild.SystemChannelFlags != 0 {
		sysSummary += fmt.Sprintf("\n• **Flags:** `%s`", formatSystemFlags(guild.SystemChannelFlags))
	}
	return &discordgo.MessageEmbedField{
		Name:   "System Channels",
		Value:  sysSummary,
		Inline: false,
	}
}

func buildServerIntegrationsField(ctx *bot.Context, guild *discordgo.Guild) *discordgo.MessageEmbedField {
	var integrations []string
	if events, err := ctx.Session.GuildScheduledEvents(guild.ID, false); err == nil && len(events) > 0 {
		integrations = append(integrations, fmt.Sprintf("• **Scheduled Events:** `%d active`", len(events)))
	}
	if ints, err := ctx.Session.GuildIntegrations(guild.ID); err == nil && len(ints) > 0 {
		integrations = append(integrations, fmt.Sprintf("• **Installed Integrations:** `%d`", len(ints)))
	}
	if guild.WidgetEnabled {
		integrations = append(integrations, fmt.Sprintf("• **Server Widget:** Enabled (<#%s>)", guild.WidgetChannelID))
	}

	if len(integrations) == 0 {
		return nil
	}

	return &discordgo.MessageEmbedField{
		Name:   "Events & Integrations",
		Value:  strings.Join(integrations, "\n"),
		Inline: false,
	}
}

func buildServerMediaLinksField(guild *discordgo.Guild, detailed bool) *discordgo.MessageEmbedField {
	var links []string
	if guild.Icon != "" {
		links = append(links, fmt.Sprintf("[Icon](%s)", guild.IconURL("4096")))
	}
	if guild.Banner != "" {
		links = append(links, fmt.Sprintf("[Banner](%s)", guild.BannerURL("4096")))
	}
	if guild.Splash != "" {
		links = append(links, fmt.Sprintf("[Splash](https://cdn.discordapp.com/splashes/%s/%s.png?size=4096)", guild.ID, guild.Splash))
	}
	if detailed && guild.DiscoverySplash != "" {
		links = append(links, fmt.Sprintf("[Discovery Splash](https://cdn.discordapp.com/discovery-splashes/%s/%s.png?size=4096)", guild.ID, guild.DiscoverySplash))
	}

	if len(links) == 0 {
		return nil
	}

	return &discordgo.MessageEmbedField{
		Name:   "Media Links",
		Value:  strings.Join(links, " • "),
		Inline: false,
	}
}

func formatVerificationLevel(level discordgo.VerificationLevel) string {
	switch level {
	case discordgo.VerificationLevelNone:
		return "None (Unrestricted)"
	case discordgo.VerificationLevelLow:
		return "Low (Verified Email)"
	case discordgo.VerificationLevelMedium:
		return "Medium (Registered >5 mins)"
	case discordgo.VerificationLevelHigh:
		return "High (Member >10 mins)"
	case discordgo.VerificationLevelVeryHigh:
		return "Highest (Verified Phone)"
	default:
		return fmt.Sprintf("Unknown (%d)", level)
	}
}

func formatExplicitFilter(filter discordgo.ExplicitContentFilterLevel) string {
	switch filter {
	case discordgo.ExplicitContentFilterDisabled:
		return "Disabled"
	case discordgo.ExplicitContentFilterMembersWithoutRoles:
		return "Members Without Roles"
	case discordgo.ExplicitContentFilterAllMembers:
		return "All Members"
	default:
		return fmt.Sprintf("Unknown (%d)", filter)
	}
}

func formatNotificationLevel(level discordgo.MessageNotifications) string {
	switch level {
	case discordgo.MessageNotificationsAllMessages:
		return "All Messages"
	case discordgo.MessageNotificationsOnlyMentions:
		return "Only @mentions"
	default:
		return fmt.Sprintf("Unknown (%d)", level)
	}
}

func formatMFALevel(level discordgo.MfaLevel) string {
	switch level {
	case discordgo.MfaLevelNone:
		return "None"
	case discordgo.MfaLevelElevated:
		return "Elevated (2FA Required for Staff)"
	default:
		return fmt.Sprintf("Unknown (%d)", level)
	}
}

func formatNSFWLevel(level discordgo.GuildNSFWLevel) string {
	switch level {
	case discordgo.GuildNSFWLevelDefault:
		return "Default"
	case discordgo.GuildNSFWLevelExplicit:
		return "Explicit"
	case discordgo.GuildNSFWLevelSafe:
		return "Safe"
	case discordgo.GuildNSFWLevelAgeRestricted:
		return "Age Restricted"
	default:
		return fmt.Sprintf("Unknown (%d)", level)
	}
}

func formatPremiumTier(tier discordgo.PremiumTier) string {
	switch tier {
	case discordgo.PremiumTierNone:
		return "Level 0"
	case discordgo.PremiumTier1:
		return "Level 1"
	case discordgo.PremiumTier2:
		return "Level 2"
	case discordgo.PremiumTier3:
		return "Level 3"
	default:
		return fmt.Sprintf("Level %d", tier)
	}
}

func formatSystemFlags(flags discordgo.SystemChannelFlag) string {
	var opts []string
	f := int(flags)
	if f&(1<<0) != 0 {
		opts = append(opts, "Mute Joins")
	}
	if f&(1<<1) != 0 {
		opts = append(opts, "Mute Boosts")
	}
	if f&(1<<2) != 0 {
		opts = append(opts, "Mute Setup Tips")
	}
	if f&(1<<3) != 0 {
		opts = append(opts, "Mute Join Stickers")
	}

	if len(opts) == 0 {
		return "All Notifications Active"
	}
	return strings.Join(opts, ", ")
}

func formatFeatures(features []discordgo.GuildFeature) string {
	caser := cases.Title(language.English)
	formatted := make([]string, 0, len(features))

	for _, f := range features {
		name := strings.ReplaceAll(string(f), "_", " ")
		formatted = append(formatted, caser.String(strings.ToLower(name)))
	}

	if len(formatted) > 12 {
		return strings.Join(formatted[:12], ", ") + fmt.Sprintf(" ... (+%d more)", len(formatted)-12)
	}
	return strings.Join(formatted, ", ")
}
