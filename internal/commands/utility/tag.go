package utility

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type TagCmd struct{}

func (c *TagCmd) Name() string        { return "tag" }
func (c *TagCmd) Aliases() []string   { return []string{"t", "tags"} }
func (c *TagCmd) Category() string    { return "Utility" }
func (c *TagCmd) Description() string { return "server tag management." }
func (c *TagCmd) Usage() string       { return "[add / remove / info / list / raw] <name> [content]" }
func (c *TagCmd) Example() string     { return "add real Cloudyy Follows all Discord ToS." }

func (c *TagCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Create a new server tag", Usage: "<name> <content>", Example: "rules Follow server ToS."},
		{Name: "delete", Description: "Delete a server tag", Usage: "<name>", Example: "rules"},
		{Name: "info", Description: "View tag metadata and author info", Usage: "<name>", Example: "rules"},
		{Name: "list", Description: "List all available server tags", Usage: "", Example: ""},
		{Name: "search", Description: "Search for server tags by keyword", Usage: "<query>", Example: "rule"},
		{Name: "raw", Description: "View unformatted raw tag source", Usage: "<name>", Example: "rules"},
	}
}

func (c *TagCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	subCmd := strings.ToLower(ctx.Args[0])
	switch subCmd {
	case "add", "create":
		return c.handleAdd(ctx)
	case "delete", "del", "remove", "rem":
		return c.handleDelete(ctx)
	case "info":
		return c.handleInfo(ctx)
	case "list", "show":
		return c.handleList(ctx)
	case "search", "find":
		return c.handleSearch(ctx)
	case "raw":
		return c.handleRaw(ctx)
	default:
		return c.handleGet(ctx, subCmd)
	}
}

func canModifyTag(ctx *bot.Context, tagAuthorID string) bool {
	if tagAuthorID == ctx.Message.Author.ID {
		return true
	}
	isAdmin, err := ctx.HasPermission(discordgo.PermissionAdministrator)
	return err == nil && isAdmin
}

func (c *TagCmd) handleGet(ctx *bot.Context, tagName string) error {
	content, err := ctx.DB.GetTagContentByGuildID(ctx.Message.GuildID, tagName)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			desc := fmt.Sprintf("Tag **%s** does not exist.", tagName)
			if matches, errSearch := ctx.DB.SearchGuildTags(ctx.Message.GuildID, tagName); errSearch == nil && len(matches) > 0 {
				var names []string
				for i, m := range matches {
					if i >= 3 {
						break
					}
					names = append(names, fmt.Sprintf("`%s`", m.TagName))
				}
				desc += fmt.Sprintf("\nDid you mean: %s?", strings.Join(names, ", "))
			}
			return ctx.SendError(desc)
		}
		return err
	}

	_, err = ctx.ReplyText(content)
	return err
}

func (c *TagCmd) handleAdd(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 3) {
		return nil
	}

	tagName := strings.ToLower(ctx.Args[1])
	content := strings.Join(ctx.Args[2:], " ")

	if len(tagName) > 32 {
		return ctx.SendError("Tag name cannot exceed 32 characters.")
	}
	if utf8.RuneCountInString(content) > 1800 {
		return ctx.SendError(fmt.Sprintf("Tag content cannot exceed 1800 characters (got %d).", utf8.RuneCountInString(content)))
	}

	existingTag, err := ctx.DB.GetTagInfoByGuildID(ctx.Message.GuildID, tagName)
	if err == nil && existingTag != nil {
		if !canModifyTag(ctx, existingTag.AuthorID) {
			return ctx.SendError(fmt.Sprintf("You do not have permission to overwrite tag **%s**. Only the tag creator (<@%s>) or an admin can edit it.", tagName, existingTag.AuthorID))
		}
	}

	if err = ctx.DB.SaveGuildTag(ctx.Message.GuildID, tagName, content, ctx.Message.Author.ID); err != nil {
		return err
	}

	return ctx.SendSuccess("Saved tag **%s**.", tagName)
}

func (c *TagCmd) handleDelete(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	tagName := strings.ToLower(ctx.Args[1])

	existingTag, err := ctx.DB.GetTagInfoByGuildID(ctx.Message.GuildID, tagName)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return ctx.SendError(fmt.Sprintf("Tag **%s** does not exist.", tagName))
		}
		return err
	}

	if !canModifyTag(ctx, existingTag.AuthorID) {
		return ctx.SendError(fmt.Sprintf("You do not have permission to delete tag **%s**. Only the tag creator (<@%s>) or an admin can delete it.", tagName, existingTag.AuthorID))
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to delete tag **%s**?", tagName))
	if err != nil || !confirmed {
		return err
	}

	if err := ctx.DB.DeleteGuildTag(ctx.Message.GuildID, tagName); err != nil {
		return err
	}

	return ctx.SendSuccess("Deleted tag **%s**.", tagName)
}

func (c *TagCmd) handleInfo(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	tagName := strings.ToLower(ctx.Args[1])

	info, err := ctx.DB.GetTagInfoByGuildID(ctx.Message.GuildID, tagName)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			embed := &discordgo.MessageEmbed{
				Description: fmt.Sprintf("Tag **%s** does not exist.", tagName),
			}
			_, errReply := ctx.ReplyEmbed(embed)
			return errReply
		}
		return err
	}

	timestamp := info.CreatedAt.Unix()

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Tag Info: %s", info.TagName),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Author",
				Value:  fmt.Sprintf("<@%s>", info.AuthorID),
				Inline: true,
			},
			{
				Name:   "Created",
				Value:  fmt.Sprintf("<t:%d:R>", timestamp),
				Inline: true,
			},
			{
				Name:   "Content Length",
				Value:  fmt.Sprintf("%d characters", len(info.Content)),
				Inline: true,
			},
		},
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *TagCmd) handleRaw(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	tagName := strings.ToLower(ctx.Args[1])

	content, err := ctx.DB.GetTagContentByGuildID(ctx.Message.GuildID, tagName)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return ctx.SendError(fmt.Sprintf("Tag **%s** does not exist.", tagName))
		}
		return err
	}

	escapedContent := strings.ReplaceAll(content, "```", "`\u200b`\u200b`")
	rawText := fmt.Sprintf("```\n%s\n```", escapedContent)
	_, err = ctx.ReplyText(rawText)
	return err
}

func (c *TagCmd) handleList(ctx *bot.Context) error {
	tags, err := ctx.DB.ListGuildTags(ctx.Message.GuildID)
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		embed := &discordgo.MessageEmbed{
			Title:       "0 Tags",
			Description: "*No tags have been created for this server yet.*",
		}
		_, err = ctx.ReplyEmbed(embed)
		return err
	}

	pageSize := 10
	totalPages := (len(tags) + pageSize - 1) / pageSize
	var embeds []*discordgo.MessageEmbed

	for page := 0; page < totalPages; page++ {
		start := page * pageSize
		end := start + pageSize
		if end > len(tags) {
			end = len(tags)
		}

		pageTags := tags[start:end]
		var lines []string

		for _, t := range pageTags {
			lines = append(lines, fmt.Sprintf("`%s`\n- Created by <@%s>", t.TagName, t.AuthorID))
		}

		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("%s Tag(s)", helpers.FormatNumber(len(tags))),
			Description: strings.Join(lines, "\n"),
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Page %d of %d", page+1, totalPages),
			},
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func (c *TagCmd) handleSearch(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	query := strings.Join(ctx.Args[1:], " ")
	tags, err := ctx.DB.SearchGuildTags(ctx.Message.GuildID, query)
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("No tags found matching query **%q**.", query),
		}
		_, err = ctx.ReplyEmbed(embed)
		return err
	}

	pageSize := 10
	totalPages := (len(tags) + pageSize - 1) / pageSize
	var embeds []*discordgo.MessageEmbed

	for page := 0; page < totalPages; page++ {
		start := page * pageSize
		end := start + pageSize
		if end > len(tags) {
			end = len(tags)
		}

		pageTags := tags[start:end]
		var lines []string

		for _, t := range pageTags {
			lines = append(lines, fmt.Sprintf("`%s`\n- Created by <@%s>", t.TagName, t.AuthorID))
		}

		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("%d Result(s) for %q", len(tags), query),
			Description: strings.Join(lines, "\n"),
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Page %d of %d", page+1, totalPages),
			},
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

type GrailCmd struct{}

func (c *GrailCmd) Name() string        { return "grail" }
func (c *GrailCmd) Aliases() []string   { return []string{"grails", "songgrails"} }
func (c *GrailCmd) Category() string    { return "Utility" }
func (c *GrailCmd) Description() string { return "Manages grail lists." }
func (c *GrailCmd) Usage() string       { return "[add / remove / clear / list] [song_name]" }
func (c *GrailCmd) Example() string     { return "add CMW, Rental, Morning Again" }

func (c *GrailCmd) Execute(ctx *bot.Context) error {
	if ctx.DB == nil {
		embed := &discordgo.MessageEmbed{
			Description: "Database connection is not initialized.",
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	subCmd := strings.ToLower(ctx.Args[0])

	parsedID := ctx.ParseUserID(ctx.Args[0])
	if len(ctx.Message.Mentions) > 0 || (parsedID != "" && len(parsedID) >= 17) {
		return c.handleList(ctx)
	}

	switch subCmd {
	case "add":
		return c.handleAdd(ctx)
	case "delete", "del", "remove", "rem":
		return c.handleDelete(ctx)
	case "clear", "reset":
		return c.handleClear(ctx)
	case "list", "show":
		return c.handleList(ctx)
	default:
		return c.handleAdd(ctx)
	}
}

func (c *GrailCmd) handleAdd(ctx *bot.Context) error {
	rawInput := ""
	if len(ctx.Args) > 0 && strings.EqualFold(ctx.Args[0], "add") {
		rawInput = strings.Join(ctx.Args[1:], " ")
	} else {
		rawInput = strings.Join(ctx.Args, " ")
	}

	if strings.Contains(ctx.Message.Content, "\n") {
		lines := strings.Split(ctx.Message.Content, "\n")
		firstLineArgs := strings.Fields(lines[0])
		if len(firstLineArgs) > 0 {
			if strings.HasPrefix(firstLineArgs[0], ctx.Prefix) {
				firstLineArgs = firstLineArgs[1:]
			}
			if len(firstLineArgs) > 0 && strings.EqualFold(firstLineArgs[0], "add") {
				firstLineArgs = firstLineArgs[1:]
			}
		}
		lines[0] = strings.Join(firstLineArgs, " ")
		rawInput = strings.Join(lines, "\n")
	}

	candidateSongs := parseSongList(rawInput)
	if len(candidateSongs) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: "Please specify the grail song(s) to add (separated by commas or newlines).",
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	existingGrails, err := ctx.DB.GetUserGrailSongs(ctx.Message.Author.ID)
	if err != nil {
		return err
	}

	existingMap := make(map[string]bool)
	for _, g := range existingGrails {
		existingMap[strings.ToLower(g)] = true
	}

	var addedSongs []string
	var duplicateSongs []string

	for _, song := range candidateSongs {
		lower := strings.ToLower(song)
		if existingMap[lower] {
			duplicateSongs = append(duplicateSongs, song)
			continue
		}
		addedSongs = append(addedSongs, song)
		existingMap[lower] = true
	}

	if len(addedSongs) > 0 {
		if err := ctx.DB.AddUserGrailSongs(ctx.Message.Author.ID, addedSongs...); err != nil {
			return err
		}
	}

	var desc string
	if len(addedSongs) > 0 {
		desc += fmt.Sprintf("Added **%s** song(s) to your grail list:\n", helpers.FormatNumber(len(addedSongs)))
		for _, s := range addedSongs {
			desc += fmt.Sprintf("• **%s**\n", s)
		}
	}

	if len(duplicateSongs) > 0 {
		if desc != "" {
			desc += "\n"
		}
		desc += fmt.Sprintf("Already in your grail list (**%s**):\n", helpers.FormatNumber(len(duplicateSongs)))
		for _, s := range duplicateSongs {
			desc += fmt.Sprintf("• *%s*\n", s)
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: desc,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *GrailCmd) handleDelete(ctx *bot.Context) error {
	if len(ctx.Args) <= 1 {
		embed := &discordgo.MessageEmbed{
			Description: "Please specify the grail song(s) to remove (separated by commas).",
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	rawInput := strings.Join(ctx.Args[1:], " ")

	candidateSongs := parseSongList(rawInput)
	if len(candidateSongs) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: "Please specify the grail song(s) to remove.",
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	userGrails, err := ctx.DB.GetUserGrailSongs(ctx.Message.Author.ID)
	if err != nil {
		return err
	}

	existingMap := make(map[string]string)
	for _, g := range userGrails {
		existingMap[strings.ToLower(g)] = g
	}

	var deletedSongs []string
	var notFoundSongs []string

	for _, song := range candidateSongs {
		lower := strings.ToLower(song)
		if exactName, ok := existingMap[lower]; ok {
			deletedSongs = append(deletedSongs, exactName)
		} else {
			notFoundSongs = append(notFoundSongs, song)
		}
	}

	if len(deletedSongs) > 0 {
		if err := ctx.DB.DeleteUserGrailSongs(ctx.Message.Author.ID, deletedSongs...); err != nil {
			return err
		}
	}

	var desc string
	if len(deletedSongs) > 0 {
		desc += fmt.Sprintf("Removed **%s** song(s) from your grail list:\n", helpers.FormatNumber(len(deletedSongs)))
		for _, s := range deletedSongs {
			desc += fmt.Sprintf("• **%s**\n", s)
		}
	}

	if len(notFoundSongs) > 0 {
		if desc != "" {
			desc += "\n"
		}
		desc += fmt.Sprintf("Not found in your grail list (**%s**):\n", helpers.FormatNumber(len(notFoundSongs)))
		for _, s := range notFoundSongs {
			desc += fmt.Sprintf("• *%s*\n", s)
		}
	}

	if desc == "" {
		desc = "No valid songs were provided to remove."
	}

	embed := &discordgo.MessageEmbed{
		Description: desc,
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *GrailCmd) handleClear(ctx *bot.Context) error {
	confirmed, err := ctx.PromptConfirmation("Are you sure you want to clear all grails? (non-recoverable)")
	if err != nil || !confirmed {
		return err
	}

	if err := ctx.DB.ClearAllUserGrails(ctx.Message.Author.ID); err != nil {
		return err
	}

	embed := &discordgo.MessageEmbed{
		Description: "Cleared all grails.",
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *GrailCmd) handleList(ctx *bot.Context) error {
	var targetUser *discordgo.User
	var targetArg string
	if len(ctx.SubArgs()) > 0 {
		targetArg = ctx.SubArgs()[0]
	} else if len(ctx.Args) > 0 && !strings.EqualFold(ctx.Args[0], "list") && !strings.EqualFold(ctx.Args[0], "show") {
		targetArg = ctx.Args[0]
	}
	if targetArg != "" {
		u, _, err := ctx.ResolveUserAndMember(targetArg)
		if err == nil && u != nil {
			targetUser = u
		}
	}
	if targetUser == nil {
		targetUser = ctx.Message.Author
	}

	userGrails, err := ctx.DB.GetUserGrailSongs(targetUser.ID)
	if err != nil {
		return err
	}

	if len(userGrails) == 0 {
		embed := &discordgo.MessageEmbed{
			Title:       "Grails for " + targetUser.DisplayName(),
			Description: "*User does not have any grails listed.*",
		}
		_, err = ctx.ReplyEmbed(embed)
		return err
	}

	var grails string
	for _, g := range userGrails {
		addition := fmt.Sprintf("• %s\n", g)
		if len(grails)+len(addition) > 4000 {
			grails += "*...and more (character limit reached)*\n"
			break
		}
		grails += addition
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Grails for " + targetUser.DisplayName(),
		Description: grails,
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func parseSongList(input string) []string {
	input = strings.ReplaceAll(input, "\n", ",")
	parts := strings.Split(input, ",")

	var songs []string
	seen := make(map[string]bool)

	for _, p := range parts {
		cleaned := strings.TrimSpace(p)
		if cleaned == "" {
			continue
		}
		lower := strings.ToLower(cleaned)
		if !seen[lower] {
			seen[lower] = true
			songs = append(songs, cleaned)
		}
	}
	return songs
}
