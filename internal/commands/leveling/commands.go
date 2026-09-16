package leveling

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/graphics"
	"gobot/internal/helpers"
	"gobot/internal/listeners"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&LevelCmd{},
	&LevelsCmd{},
	&LeaderboardCmd{},
	&AddlevelroleCmd{},
	&RemovelevelroleCmd{},
	&SynclevelsCmd{},
	&LevelupmsgCmd{},
	&AddxpCmd{},
	&SetxpCmd{},
	&RemovexpCmd{},
	&SetlevelCmd{},
	&ResetxpCmd{},
}

type LevelCmd struct{}

func (c *LevelCmd) Name() string      { return "level" }
func (c *LevelCmd) Aliases() []string { return []string{"xp", "rank"} }
func (c *LevelCmd) Category() string  { return "Leveling" }
func (c *LevelCmd) Description() string {
	return "Check XP and level for yourself or a member."
}
func (c *LevelCmd) Usage() string   { return "(@user)" }
func (c *LevelCmd) Example() string { return "@Cloudyy" }

func (c *LevelCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	targetUser, _, _ := ctx.TargetUserAndMember()
	if targetUser == nil {
		targetUser = ctx.Message.Author
	}

	data, err := ctx.DB.GetUserXP(ctx.Message.GuildID, targetUser.ID)
	if err != nil {
		return ctx.SendError("Could not fetch user XP level.")
	}

	prevLevelXP := database.RequiredXPForLevel(data.Level)
	nextLevelXP := database.RequiredXPForLevel(data.Level + 1)
	_, currentLevelXP, rangeXP, progressPct := database.XPProgress(data.XP)

	rank := 1
	if r, errRank := ctx.DB.GetUserServerRank(ctx.Message.GuildID, targetUser.ID); errRank == nil {
		rank = r
	}

	cardBytes, errCard := graphics.GenerateLevelCard(graphics.LevelCardData{
		Username:      targetUser.DisplayName(),
		Discriminator: targetUser.Discriminator,
		ServerRank:    rank,
		Level:         data.Level,
		CurrentXP:     int(data.XP),
		NextLevelXP:   int(nextLevelXP),
		PrevLevelXP:   int(prevLevelXP),
		AvatarURL:     helpers.UserAvatar(targetUser),
	})

	if errCard == nil && len(cardBytes) > 0 {
		_, err = ctx.SendFile("rank_card.png", bytes.NewReader(cardBytes))
		return err
	}

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s's Level & Rank", targetUser.DisplayName()),
			IconURL: helpers.UserAvatar(targetUser),
		},
		Color: helpers.ColorDefault,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Level", Value: fmt.Sprintf("**%s**", helpers.FormatNumber(data.Level)), Inline: true},
			{Name: "Total XP", Value: fmt.Sprintf("**%s** XP", helpers.FormatNumber(int(data.XP))), Inline: true},
			{Name: "Progress", Value: fmt.Sprintf("**%s** / **%s** XP (%.1f%%)", helpers.FormatNumber(int(currentLevelXP)), helpers.FormatNumber(int(rangeXP)), progressPct), Inline: true},
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type LevelsCmd struct{}

func (c *LevelsCmd) Name() string      { return "levels" }
func (c *LevelsCmd) Aliases() []string { return []string{"levelinfo", "levelconfig"} }
func (c *LevelsCmd) Category() string  { return "Leveling" }
func (c *LevelsCmd) Description() string {
	return "Setup the leveling system or view a user's level progress."
}
func (c *LevelsCmd) Usage() string {
	return "[user | setrate <multiplier> | ignore <#channel|list> | stack <true|false>]"
}

func (c *LevelsCmd) Example() string {
	return "setrate 1.5"
}

func (c *LevelsCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{
			Name:        "setrate",
			Description: "Set the server XP gain multiplier",
			Usage:       "<multiplier>",
			Example:     "1.5",
		},
		{
			Name:        "ignore",
			Description: "Toggle channel XP earning or view the ignored channels list",
			Usage:       "<#channel | list>",
			Example:     "#bot-spam",
		},
		{
			Name:        "stack",
			Description: "Set level role rewards to stack or replace lower roles",
			Usage:       "<true|false>",
			Example:     "true",
		},
	}
}

func (c *LevelsCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		var levelCmd LevelCmd
		return levelCmd.Execute(ctx)
	}

	sub := strings.ToLower(ctx.Args[0])
	switch sub {
	case "setrate", "multiplier", "rate":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
			return nil
		}
		if !ctx.RequireSubArgs(2, "levels setrate <multiplier> (e.g. 1.5)") {
			return nil
		}
		var mult float64
		if _, err := fmt.Sscanf(ctx.Args[1], "%f", &mult); err != nil || mult < 0.01 || mult > 100.0 {
			return ctx.SendError("Please specify a valid numeric multiplier between 0.01 and 100.0.")
		}
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingXPMultiplier, fmt.Sprintf("%.2f", mult)); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update multiplier in database: %v", err))
		}
		return ctx.SendSuccess("Set XP gain multiplier to **%.2fx**.", mult)

	case "ignore":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
			return nil
		}
		if !ctx.RequireSubArgs(2, "levels ignore <#channel | list>") {
			return nil
		}
		if strings.ToLower(ctx.Args[1]) == "list" {
			ignored, _ := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingIgnoredLevelChannelIDs)
			var mentions []string
			for _, id := range ignored {
				mentions = append(mentions, fmt.Sprintf("<#%s>", id))
			}
			val := "None"
			if len(mentions) > 0 {
				val = strings.Join(mentions, ", ")
			}
			embed := &discordgo.MessageEmbed{
				Title:       "Ignored XP Channels",
				Description: val,
			}
			_, err := ctx.ReplyEmbed(embed)
			return err
		}

		ch, err := ctx.ResolveChannel(ctx.Args[1])
		if err != nil || ch == nil {
			return ctx.SendError("Invalid channel specified. Please mention a valid text channel.")
		}
		chID := ch.ID
		ignored, errIgn := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingIgnoredLevelChannelIDs)
		if errIgn != nil && !errors.Is(errIgn, database.ErrNotFound) {
			return ctx.SendError("Failed to retrieve ignored channels from database.")
		}
		var updated []string
		found := false
		for _, id := range ignored {
			if id == chID {
				found = true
				continue
			}
			updated = append(updated, id)
		}

		if found {
			if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingIgnoredLevelChannelIDs, updated); err != nil {
				return ctx.SendError(fmt.Sprintf("Failed to update ignored channel in database: %v", err))
			}
			return ctx.SendSuccess("Unignored <#%s> from earning XP.", chID)
		}

		updated = append(updated, chID)
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingIgnoredLevelChannelIDs, updated); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update ignored channel in database: %v", err))
		}
		return ctx.SendSuccess("Ignored <#%s> from earning XP.", chID)

	case "stack":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
			return nil
		}
		if !ctx.RequireSubArgs(2, "levels stack <true|false>") {
			return nil
		}
		stackBool, ok := ctx.ParseBool(ctx.Args[1])
		if !ok {
			return ctx.SendError("Set to 'true' to stack roles, or 'false' to replace lower roles.")
		}
		isStack := "true"
		if !stackBool {
			isStack = "false"
		}
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingLevelRolesStack, isStack); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update level roles stack setting in database: %v", err))
		}

		statusText := "Level reward roles will now **stack**."
		if !stackBool {
			statusText = "Level reward roles will now **replace** lower level roles."
		}
		return ctx.SendSuccess("%s", statusText)

	default:
		var levelCmd LevelCmd
		return levelCmd.Execute(ctx)
	}
}

type LeaderboardCmd struct{}

func (c *LeaderboardCmd) Name() string        { return "leaderboard" }
func (c *LeaderboardCmd) Aliases() []string   { return []string{"xpleaderboard", "topxps"} }
func (c *LeaderboardCmd) Category() string    { return "Leveling" }
func (c *LeaderboardCmd) Description() string { return "View the top server XP leaderboard." }
func (c *LeaderboardCmd) Usage() string       { return "" }
func (c *LeaderboardCmd) Example() string     { return "" }

func (c *LeaderboardCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	topUsers, err := ctx.DB.GetTopXPLeaderboard(ctx.Message.GuildID, 10)
	if err != nil || len(topUsers) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: "No XP records recorded for this server yet.",
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	var sb strings.Builder
	for i, u := range topUsers {
		sb.WriteString(fmt.Sprintf("`#%d` <@%s> - Level **%s** (%s XP)\n", i+1, u.UserID, helpers.FormatNumber(u.Level), helpers.FormatNumber(int(u.XP))))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Server XP Leaderboard",
		Description: sb.String(),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type AddlevelroleCmd struct{}

func (c *AddlevelroleCmd) Name() string        { return "addlevelrole" }
func (c *AddlevelroleCmd) Aliases() []string   { return []string{"setlevelrole"} }
func (c *AddlevelroleCmd) Category() string    { return "Leveling" }
func (c *AddlevelroleCmd) Description() string { return "Link a role reward to a specific level." }
func (c *AddlevelroleCmd) Usage() string       { return "<level> <@role>" }
func (c *AddlevelroleCmd) Example() string     { return "5 @VIP" }
func (c *AddlevelroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *AddlevelroleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	levelNum, err := strconv.Atoi(ctx.Args[0])
	if err != nil || levelNum < 1 {
		return ctx.SendError("Please specify a valid numeric level.")
	}

	role, err := ctx.ResolveRole(ctx.Args[1])
	if err != nil || role == nil {
		return ctx.SendError("Please specify a valid role.")
	}

	if err := ctx.DB.AddLevelRole(ctx.Message.GuildID, levelNum, role.ID); err != nil {
		bot.Errorf("[LEVELING] Failed to add level role: %v", err)
		return ctx.SendError("Failed to save level role reward to database.")
	}
	listeners.InvalidateGuildLevelRolesCache(ctx.Message.GuildID)

	return ctx.SendSuccess("Linked Level **%d** reward to role **%s** (`%s`).", levelNum, role.Name, role.ID)
}

type RemovelevelroleCmd struct{}

func (c *RemovelevelroleCmd) Name() string        { return "removelevelrole" }
func (c *RemovelevelroleCmd) Aliases() []string   { return []string{"dellevelrole"} }
func (c *RemovelevelroleCmd) Category() string    { return "Leveling" }
func (c *RemovelevelroleCmd) Description() string { return "Remove a role reward for a level." }
func (c *RemovelevelroleCmd) Usage() string       { return "<level>" }
func (c *RemovelevelroleCmd) Example() string     { return "5" }
func (c *RemovelevelroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *RemovelevelroleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	levelNum, err := strconv.Atoi(ctx.Args[0])
	if err != nil || levelNum < 1 {
		return ctx.SendError("Please specify a valid numeric level.")
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to remove the Level **%d** role reward?", levelNum))
	if err != nil || !confirmed {
		return err
	}

	if err := ctx.DB.RemoveLevelRole(ctx.Message.GuildID, levelNum); err != nil {
		bot.Errorf("[LEVELING] Failed to remove level role: %v", err)
		return ctx.SendError("Failed to remove level role reward from database.")
	}
	listeners.InvalidateGuildLevelRolesCache(ctx.Message.GuildID)

	return ctx.SendSuccess("Removed Level **%d** role reward.", levelNum)
}

type SynclevelsCmd struct{}

func (c *SynclevelsCmd) Name() string        { return "synclevels" }
func (c *SynclevelsCmd) Aliases() []string   { return []string{"recalculatepoles"} }
func (c *SynclevelsCmd) Category() string    { return "Leveling" }
func (c *SynclevelsCmd) Description() string { return "Synchronize level roles for server members." }
func (c *SynclevelsCmd) Usage() string       { return "" }
func (c *SynclevelsCmd) Example() string     { return "" }
func (c *SynclevelsCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *SynclevelsCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	rewards, err := ctx.DB.GetLevelRoles(ctx.Message.GuildID)
	if err != nil || len(rewards) == 0 {
		return ctx.SendError("No level role rewards are configured for this server.")
	}

	users, err := ctx.DB.GetAllGuildUserXP(ctx.Message.GuildID)
	if err != nil || len(users) == 0 {
		return ctx.SendError("No user XP records found for this server.")
	}

	prompt := fmt.Sprintf("Are you sure you want to synchronize **%s** level role rewards across **%s** members with XP records? This process will run safely in the background.", helpers.FormatNumber(len(rewards)), helpers.FormatNumber(len(users)))
	confirmed, err := ctx.PromptConfirmation(prompt)
	if err != nil || !confirmed {
		return err
	}

	s := ctx.Session
	db := ctx.DB
	guildID := ctx.Message.GuildID
	botCtx := ctx.Context()
	helpers.Spawn(func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for _, u := range users {
			select {
			case <-botCtx.Done():
				return
			case <-ticker.C:
				_ = listeners.SyncUserLevelRoles(s, db, guildID, u.UserID)
			}
		}
	})

	return ctx.SendSuccess("Started level role synchronization for **%s** members in background.", helpers.FormatNumber(len(users)))
}

type LevelupmsgCmd struct{}

func (c *LevelupmsgCmd) Name() string        { return "levelupmsg" }
func (c *LevelupmsgCmd) Aliases() []string   { return []string{"levelupmessage", "togglevelups"} }
func (c *LevelupmsgCmd) Category() string    { return "Leveling" }
func (c *LevelupmsgCmd) Description() string { return "Toggles level-up announcement messages." }
func (c *LevelupmsgCmd) Usage() string       { return "[enable / disable / status]" }
func (c *LevelupmsgCmd) Example() string     { return "disable" }
func (c *LevelupmsgCmd) Permissions() int64  { return discordgo.PermissionManageGuild }

func (c *LevelupmsgCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if len(ctx.Args) == 0 || strings.EqualFold(ctx.Args[0], "status") {
		enabled, _ := ctx.DB.GetGuildSettingBool(ctx.Message.GuildID, database.SettingLevelUpMessagesEnabled)
		return ctx.SendSuccess("Level-up announcement messages are currently **%s**.", helpers.FormatEnabled(enabled))
	}

	isEnable, ok := ctx.ParseBool(ctx.Args[0])
	if !ok {
		return ctx.SendError("Please specify `enable`, `disable`, or `status`.")
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingLevelUpMessagesEnabled, isEnable); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}

	return ctx.SendSuccess("Level-up announcement messages are now **%s**.", helpers.FormatEnabled(isEnable))
}

type AddxpCmd struct{}

func (c *AddxpCmd) Name() string        { return "addxp" }
func (c *AddxpCmd) Aliases() []string   { return []string{"givexp"} }
func (c *AddxpCmd) Category() string    { return "Leveling" }
func (c *AddxpCmd) Description() string { return "Adds experience points to a user." }
func (c *AddxpCmd) Usage() string       { return "<member> <xp>" }
func (c *AddxpCmd) Example() string     { return "@Cloudyy 500" }
func (c *AddxpCmd) Permissions() int64  { return discordgo.PermissionManageGuild }

func (c *AddxpCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid server member.")
	}

	amount, err := strconv.ParseInt(ctx.Args[1], 10, 64)
	if err != nil || amount <= 0 {
		return ctx.SendError("Please specify a positive numeric XP amount.")
	}
	const maxGrantXP = 100_000_000
	if amount > maxGrantXP {
		return ctx.SendError("XP amount exceeds maximum allowed grant (100,000,000 XP).")
	}

	newLevel, _, err := ctx.DB.AddUserXP(ctx.Message.GuildID, targetUser.ID, amount)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to add XP: %v", err))
	}

	_ = listeners.SyncUserLevelRoles(ctx.Session, ctx.DB, ctx.Message.GuildID, targetUser.ID)
	return ctx.SendSuccess("Added **%s** XP to %s. (New Level: **%s**)", helpers.FormatNumber64(amount), targetUser.Mention(), helpers.FormatNumber(newLevel))
}

type SetxpCmd struct{}

func (c *SetxpCmd) Name() string        { return "setxp" }
func (c *SetxpCmd) Aliases() []string   { return []string{"setuserxp"} }
func (c *SetxpCmd) Category() string    { return "Leveling" }
func (c *SetxpCmd) Description() string { return "Sets a user's exact experience points." }
func (c *SetxpCmd) Usage() string       { return "<member> <xp>" }
func (c *SetxpCmd) Example() string     { return "@Cloudyy 1200" }
func (c *SetxpCmd) Permissions() int64  { return discordgo.PermissionManageGuild }

func (c *SetxpCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid server member.")
	}

	amount, err := strconv.ParseInt(ctx.Args[1], 10, 64)
	if err != nil || amount < 0 {
		return ctx.SendError("Please specify a valid non-negative numeric XP amount.")
	}

	applied := amount
	capped := false
	if applied > database.MaxSafeXP {
		applied = database.MaxSafeXP
		capped = true
	}

	newLevel, err := ctx.DB.SetUserXP(ctx.Message.GuildID, targetUser.ID, applied)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to set XP: %v", err))
	}

	_ = listeners.SyncUserLevelRoles(ctx.Session, ctx.DB, ctx.Message.GuildID, targetUser.ID)

	desc := fmt.Sprintf("Set %s's experience points to **%s** XP. (Level: **%s**)", targetUser.Mention(), helpers.FormatNumber64(applied), helpers.FormatNumber(newLevel))
	if capped {
		desc += fmt.Sprintf("\n-# Requested %s XP was capped at the maximum of %s.", helpers.FormatNumber64(amount), helpers.FormatNumber64(database.MaxSafeXP))
	}

	return ctx.SendSuccess("%s", desc)
}

type RemovexpCmd struct{}

func (c *RemovexpCmd) Name() string        { return "removexp" }
func (c *RemovexpCmd) Aliases() []string   { return []string{"takexp", "delxp"} }
func (c *RemovexpCmd) Category() string    { return "Leveling" }
func (c *RemovexpCmd) Description() string { return "Removes experience points from a user." }
func (c *RemovexpCmd) Usage() string       { return "<member> <xp>" }
func (c *RemovexpCmd) Example() string     { return "@Cloudyy 300" }
func (c *RemovexpCmd) Permissions() int64  { return discordgo.PermissionManageGuild }

func (c *RemovexpCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid server member.")
	}

	amount, err := strconv.ParseInt(ctx.Args[1], 10, 64)
	if err != nil || amount <= 0 {
		return ctx.SendError("Please specify a positive numeric XP amount.")
	}

	newLevel, newXP, err := ctx.DB.RemoveUserXP(ctx.Message.GuildID, targetUser.ID, amount)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to remove XP: %v", err))
	}

	_ = listeners.SyncUserLevelRoles(ctx.Session, ctx.DB, ctx.Message.GuildID, targetUser.ID)
	return ctx.SendSuccess("Removed **%s** XP from %s. (Current: **%s** XP, Level: **%s**)", helpers.FormatNumber64(amount), targetUser.Mention(), helpers.FormatNumber64(newXP), helpers.FormatNumber(newLevel))
}

type SetlevelCmd struct{}

func (c *SetlevelCmd) Name() string        { return "setlevel" }
func (c *SetlevelCmd) Aliases() []string   { return []string{"setuserlevel"} }
func (c *SetlevelCmd) Category() string    { return "Leveling" }
func (c *SetlevelCmd) Description() string { return "Sets a user's exact level." }
func (c *SetlevelCmd) Usage() string       { return "<member> <level>" }
func (c *SetlevelCmd) Example() string     { return "@Cloudyy 10" }
func (c *SetlevelCmd) Permissions() int64  { return discordgo.PermissionManageGuild }

func (c *SetlevelCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid server member.")
	}

	targetLevel, err := strconv.Atoi(ctx.Args[1])
	if err != nil || targetLevel < 1 || targetLevel > database.MaxLevel {
		return ctx.SendError(fmt.Sprintf("Please specify a valid numeric level between 1 and %d.", database.MaxLevel))
	}

	xp, err := ctx.DB.SetUserLevel(ctx.Message.GuildID, targetUser.ID, targetLevel)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to set level: %v", err))
	}

	_ = listeners.SyncUserLevelRoles(ctx.Session, ctx.DB, ctx.Message.GuildID, targetUser.ID)
	return ctx.SendSuccess("Set %s's level to **Level %s** (%s XP).", targetUser.Mention(), helpers.FormatNumber(targetLevel), helpers.FormatNumber64(xp))
}

type ResetxpCmd struct{}

func (c *ResetxpCmd) Name() string      { return "resetxp" }
func (c *ResetxpCmd) Aliases() []string { return []string{"resetlevel", "clearxp"} }
func (c *ResetxpCmd) Category() string  { return "Leveling" }
func (c *ResetxpCmd) Description() string {
	return "Resets a user's level and experience back to zero."
}
func (c *ResetxpCmd) Usage() string      { return "<member>" }
func (c *ResetxpCmd) Example() string    { return "@Cloudyy" }
func (c *ResetxpCmd) Permissions() int64 { return discordgo.PermissionManageGuild }

func (c *ResetxpCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid server member.")
	}

	prompt := fmt.Sprintf("Are you sure you want to reset all level and XP data for **%s**? This action cannot be undone.", targetUser.Username)
	confirmed, err := ctx.PromptConfirmation(prompt)
	if err != nil || !confirmed {
		return err
	}

	if err := ctx.DB.ResetUserXP(ctx.Message.GuildID, targetUser.ID); err != nil {
		bot.Errorf("[LEVELING] Failed to reset XP for user %s: %v", targetUser.ID, err)
		return ctx.SendError("Failed to reset member XP records in database.")
	}

	_ = listeners.SyncUserLevelRoles(ctx.Session, ctx.DB, ctx.Message.GuildID, targetUser.ID)
	return ctx.SendSuccess("Reset level and experience points for %s back to zero.", targetUser.Mention())
}
