package utility

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

type HelpCmd struct {
	Registry *commands.Registry
}

func (c *HelpCmd) Name() string      { return "help" }
func (c *HelpCmd) Aliases() []string { return []string{"h", "cmds", "commands"} }
func (c *HelpCmd) Category() string  { return "Utility" }
func (c *HelpCmd) Description() string {
	return "Display help menu or detailed command info."
}
func (c *HelpCmd) Usage() string   { return "[command | category]" }
func (c *HelpCmd) Example() string { return "ban" }

func (c *HelpCmd) Execute(ctx *bot.Context) error {
	if c.Registry == nil {
		return ctx.SendError("Command registry is uninitialized.")
	}

	allCmds := c.Registry.AllCommands()
	if len(allCmds) == 0 {
		return ctx.SendError("No commands registered.")
	}

	if len(ctx.Args) > 0 {
		fullQuery := strings.ToLower(strings.Join(ctx.Args, " "))
		firstWord := strings.ToLower(ctx.Args[0])

		if targetCmd, found := c.Registry.Get(firstWord); found {
			subQuery := ""
			if len(ctx.Args) > 1 {
				subQuery = strings.ToLower(strings.Join(ctx.Args[1:], " "))
			}
			return c.renderCommandDetail(ctx, targetCmd, subQuery)
		}

		categoryMap := make(map[string][]commands.Command)
		for _, cmd := range allCmds {
			cat := cmd.Category()
			if cat == "" {
				cat = "Utility"
			}
			categoryMap[cat] = append(categoryMap[cat], cmd)
		}

		var sortedCatNames []string
		for catName := range categoryMap {
			sortedCatNames = append(sortedCatNames, catName)
		}
		sort.Strings(sortedCatNames)

		for _, catName := range sortedCatNames {
			if strings.EqualFold(catName, fullQuery) {
				embeds := buildCategoryEmbeds(ctx.Prefix, catName, categoryMap[catName])
				return ctx.SendPaginatedEmbeds(embeds)
			}
		}
		for _, catName := range sortedCatNames {
			if strings.HasPrefix(strings.ToLower(catName), fullQuery) {
				embeds := buildCategoryEmbeds(ctx.Prefix, catName, categoryMap[catName])
				return ctx.SendPaginatedEmbeds(embeds)
			}
		}

		var bestMatchCmd commands.Command
		var bestMatchSubName string
		bestSubLen := -1

		for _, cmd := range allCmds {
			if subProvider, ok := cmd.(commands.Subcommandable); ok {
				for _, sub := range subProvider.Subcommands() {
					subNameLower := strings.ToLower(sub.Name)
					if subNameLower == fullQuery {
						return c.renderCommandDetail(ctx, cmd, subNameLower)
					}
					if strings.HasPrefix(fullQuery, subNameLower) || strings.HasPrefix(subNameLower, fullQuery) {
						if len(subNameLower) > bestSubLen {
							bestSubLen = len(subNameLower)
							bestMatchCmd = cmd
							bestMatchSubName = subNameLower
						}
					}
				}
			}
		}

		if bestMatchCmd != nil {
			return c.renderCommandDetail(ctx, bestMatchCmd, bestMatchSubName)
		}
	}

	return c.renderInteractiveMenu(ctx, allCmds)
}

func (c *HelpCmd) renderCommandDetail(ctx *bot.Context, cmd commands.Command, subQuery string) error {
	cmdPrefix := ctx.Prefix
	if _, ok := cmd.(commands.SlashOnly); ok {
		cmdPrefix = "/"
	}

	aliasesStr := "None"
	if len(cmd.Aliases()) > 0 {
		aliasesStr = strings.Join(cmd.Aliases(), ", ")
	}

	usageStr := fmt.Sprintf("`%s%s`", cmdPrefix, cmd.Name())
	if strings.TrimSpace(cmd.Usage()) != "" {
		usageStr = fmt.Sprintf("`%s%s %s`", cmdPrefix, cmd.Name(), cmd.Usage())
	}

	exampleStr := fmt.Sprintf("`%s%s`", cmdPrefix, cmd.Name())
	if strings.TrimSpace(cmd.Example()) != "" {
		exampleStr = fmt.Sprintf("`%s%s %s`", cmdPrefix, cmd.Name(), cmd.Example())
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Category", Value: cmd.Category(), Inline: true},
		{Name: "Aliases", Value: aliasesStr, Inline: true},
		{Name: "Usage", Value: usageStr, Inline: false},
		{Name: "Example", Value: exampleStr, Inline: false},
	}

	if subProvider, ok := cmd.(commands.Subcommandable); ok {
		subList := subProvider.Subcommands()
		if len(subList) > 0 {
			if matchingSub := findMatchingSubcommand(subList, subQuery); matchingSub != nil {
				return renderSubcommandDetail(ctx, cmd, matchingSub, subList, cmdPrefix)
			}
			fields = append(fields, buildSubcommandsListFields(cmd, subList, cmdPrefix)...)
		}
	}

	if helpFieldProvider, ok := cmd.(commands.HelpFieldsProvider); ok {
		fields = append(fields, helpFieldProvider.HelpFields()...)
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Command: %s%s", cmdPrefix, cmd.Name()),
		Description: cmd.Description(),
		Fields:      fields,
	}

	_, err := ctx.ReplyEmbed(embed)
	return err
}

func findMatchingSubcommand(subList []commands.Subcommand, subQuery string) *commands.Subcommand {
	if subQuery == "" {
		return nil
	}
	cleanSubQuery := strings.ToLower(strings.TrimSpace(subQuery))

	for i := range subList {
		if strings.ToLower(subList[i].Name) == cleanSubQuery {
			return &subList[i]
		}
	}

	bestLen := -1
	var matchingSub *commands.Subcommand
	for i := range subList {
		subNameLower := strings.ToLower(subList[i].Name)
		if strings.HasPrefix(cleanSubQuery, subNameLower) || strings.HasPrefix(subNameLower, cleanSubQuery) {
			if len(subNameLower) > bestLen {
				bestLen = len(subNameLower)
				matchingSub = &subList[i]
			}
		}
	}
	return matchingSub
}

func renderSubcommandDetail(ctx *bot.Context, cmd commands.Command, matchingSub *commands.Subcommand, subList []commands.Subcommand, cmdPrefix string) error {
	subUsageStr := fmt.Sprintf("`%s%s %s`", cmdPrefix, cmd.Name(), matchingSub.Name)
	if strings.TrimSpace(matchingSub.Usage) != "" {
		subUsageStr = fmt.Sprintf("`%s%s %s %s`", cmdPrefix, cmd.Name(), matchingSub.Name, matchingSub.Usage)
	}

	subExampleStr := fmt.Sprintf("`%s%s %s`", cmdPrefix, cmd.Name(), matchingSub.Name)
	if strings.TrimSpace(matchingSub.Example) != "" {
		subExampleStr = fmt.Sprintf("`%s%s %s %s`", cmdPrefix, cmd.Name(), matchingSub.Name, matchingSub.Example)
	}

	subFields := []*discordgo.MessageEmbedField{
		{Name: "Parent Command", Value: fmt.Sprintf("`%s%s`", cmdPrefix, cmd.Name()), Inline: true},
		{Name: "Category", Value: cmd.Category(), Inline: true},
		{Name: "Usage", Value: subUsageStr, Inline: false},
		{Name: "Example", Value: subExampleStr, Inline: false},
	}

	var childSubs []commands.Subcommand
	parentPrefix := strings.ToLower(matchingSub.Name) + " "
	for _, sub := range subList {
		if strings.HasPrefix(strings.ToLower(sub.Name), parentPrefix) {
			childSubs = append(childSubs, sub)
		}
	}

	if len(childSubs) > 0 {
		sort.Slice(childSubs, func(i, j int) bool {
			return childSubs[i].Name < childSubs[j].Name
		})
		var childSb strings.Builder
		for _, child := range childSubs {
			cDesc := child.Description
			if cDesc == "" {
				cDesc = "No description available"
			}
			childSb.WriteString(fmt.Sprintf("> `%s%s %s` - %s\n", cmdPrefix, cmd.Name(), child.Name, cDesc))
		}
		subFields = append(subFields, &discordgo.MessageEmbedField{
			Name:   "Subcommands",
			Value:  childSb.String(),
			Inline: false,
		})
	}

	if helpFieldProvider, ok := cmd.(commands.HelpFieldsProvider); ok {
		subFields = append(subFields, helpFieldProvider.HelpFields()...)
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Subcommand: %s%s %s", cmdPrefix, cmd.Name(), matchingSub.Name),
		Description: matchingSub.Description,
		Fields:      subFields,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func buildSubcommandsListFields(cmd commands.Command, subList []commands.Subcommand, cmdPrefix string) []*discordgo.MessageEmbedField {
	sortedSubs := make([]commands.Subcommand, len(subList))
	copy(sortedSubs, subList)
	sort.Slice(sortedSubs, func(i, j int) bool {
		return sortedSubs[i].Name < sortedSubs[j].Name
	})

	var fields []*discordgo.MessageEmbedField
	var currentField strings.Builder
	fieldName := "Subcommands"
	for _, sub := range sortedSubs {
		desc := sub.Description
		if desc == "" {
			desc = "No description available"
		}
		line := fmt.Sprintf("> `%s%s %s` - %s\n", cmdPrefix, cmd.Name(), sub.Name, desc)
		if currentField.Len()+len(line) > 1000 {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   fieldName,
				Value:  currentField.String(),
				Inline: false,
			})
			currentField.Reset()
			fieldName = "Subcommands (cont.)"
		}
		currentField.WriteString(line)
	}
	if currentField.Len() > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fieldName,
			Value:  currentField.String(),
			Inline: false,
		})
	}
	return fields
}

type helpMenuState struct {
	mu           sync.Mutex
	activeCat    string
	activePage   int
	activeEmbeds []*discordgo.MessageEmbed
}

func (c *HelpCmd) renderInteractiveMenu(ctx *bot.Context, allCmds []commands.Command) error {
	categoryMap, categories := prepareCategoryMap(allCmds)

	selectCustomID := fmt.Sprintf("help_select_%s", ctx.Message.ID)
	prevCustomID := fmt.Sprintf("help_prev_%s", ctx.Message.ID)
	nextCustomID := fmt.Sprintf("help_next_%s", ctx.Message.ID)

	state := &helpMenuState{
		activeCat:  "HOME",
		activePage: 0,
		activeEmbeds: []*discordgo.MessageEmbed{
			buildHomeEmbed(ctx.Prefix, categories, categoryMap, len(allCmds), ctx.Policy, ctx.Message.GuildID),
		},
	}

	msg, err := ctx.Session.ChannelMessageSendComplex(ctx.Message.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{state.activeEmbeds[0]},
		Components: buildHelpComponents(helpComponentsOptions{
			selectCustomID: selectCustomID,
			prevCustomID:   prevCustomID,
			nextCustomID:   nextCustomID,
			categories:     categories,
			categoryMap:    categoryMap,
			activeCategory: state.activeCat,
			pageIndex:      state.activePage,
			totalPages:     len(state.activeEmbeds),
		}),
	})
	if err != nil {
		return err
	}

	activity := make(chan struct{}, 1)
	timer := time.NewTimer(helpers.DurationPagination)

	handler := newHelpMenuInteractionHandler(ctx, msg.ID, state, categories, categoryMap, allCmds, selectCustomID, prevCustomID, nextCustomID, activity)
	removeHandler := ctx.Session.AddHandler(handler)

	startHelpMenuTimeoutWatchdog(ctx, msg.ID, timer, activity, removeHandler)
	return nil
}

func prepareCategoryMap(allCmds []commands.Command) (map[string][]commands.Command, []string) {
	categoryMap := make(map[string][]commands.Command)
	for _, cmd := range allCmds {
		cat := cmd.Category()
		if cat == "" {
			cat = "Utility"
		}
		categoryMap[cat] = append(categoryMap[cat], cmd)
	}

	categories := make([]string, 0, len(categoryMap))
	for cat := range categoryMap {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	for cat := range categoryMap {
		sort.Slice(categoryMap[cat], func(i, j int) bool {
			return categoryMap[cat][i].Name() < categoryMap[cat][j].Name()
		})
	}
	return categoryMap, categories
}

func newHelpMenuInteractionHandler(
	ctx *bot.Context,
	msgID string,
	state *helpMenuState,
	categories []string,
	categoryMap map[string][]commands.Command,
	allCmds []commands.Command,
	selectCustomID, prevCustomID, nextCustomID string,
	activity chan<- struct{},
) func(*discordgo.Session, *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionMessageComponent || i.Message == nil || i.Message.ID != msgID {
			return
		}

		var userID string
		var memberRoles []string
		if i.Member != nil {
			memberRoles = i.Member.Roles
			if i.Member.User != nil {
				userID = i.Member.User.ID
			}
		}
		if userID == "" && i.User != nil {
			userID = i.User.ID
		}

		if userID != ctx.Message.Author.ID {
			helpers.RespondEphemeral(s, i, "Only the command author can use these navigation controls.")
			return
		}

		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       i.GuildID,
			ChannelID:     i.ChannelID,
			UserID:        userID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()

		data := i.MessageComponentData()
		customID := data.CustomID

		if customID == selectCustomID {
			if len(data.Values) > 0 {
				val := data.Values[0]
				state.activeCat = val
				state.activePage = 0

				if val == "HOME" {
					state.activeEmbeds = []*discordgo.MessageEmbed{buildHomeEmbed(ctx.Prefix, categories, categoryMap, len(allCmds), ctx.Policy, ctx.Message.GuildID)}
				} else if cmds, exists := categoryMap[val]; exists {
					state.activeEmbeds = buildCategoryEmbeds(ctx.Prefix, val, cmds)
				} else {
					state.activeCat = "HOME"
					state.activeEmbeds = []*discordgo.MessageEmbed{buildHomeEmbed(ctx.Prefix, categories, categoryMap, len(allCmds), ctx.Policy, ctx.Message.GuildID)}
				}
			}
		} else if customID == prevCustomID && len(state.activeEmbeds) > 1 {
			state.activePage = (state.activePage - 1 + len(state.activeEmbeds)) % len(state.activeEmbeds)
		} else if customID == nextCustomID && len(state.activeEmbeds) > 1 {
			state.activePage = (state.activePage + 1) % len(state.activeEmbeds)
		} else {
			return
		}

		if state.activePage < 0 || state.activePage >= len(state.activeEmbeds) {
			state.activePage = 0
		}

		targetEmbed := state.activeEmbeds[state.activePage]

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{targetEmbed},
				Components: buildHelpComponents(helpComponentsOptions{
					selectCustomID: selectCustomID,
					prevCustomID:   prevCustomID,
					nextCustomID:   nextCustomID,
					categories:     categories,
					categoryMap:    categoryMap,
					activeCategory: state.activeCat,
					pageIndex:      state.activePage,
					totalPages:     len(state.activeEmbeds),
				}),
			},
		})

		select {
		case activity <- struct{}{}:
		default:
		}
	}
}

func startHelpMenuTimeoutWatchdog(ctx *bot.Context, msgID string, timer *time.Timer, activity <-chan struct{}, removeHandler func()) {
	helpers.Spawn(func() {
		for {
			select {
			case <-ctx.Context().Done():
				timer.Stop()
				removeHandler()
				return
			case <-timer.C:
				removeHandler()
				_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    ctx.Message.ChannelID,
					ID:         msgID,
					Components: &[]discordgo.MessageComponent{},
				})
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(helpers.DurationPagination)
			}
		}
	})
}

func buildHomeEmbed(prefix string, categories []string, categoryMap map[string][]commands.Command, totalCmds int, pol *policy.Module, guildID string) *discordgo.MessageEmbed {
	var sb strings.Builder
	sb.WriteString("Select a category from the dropdown menu below to view available commands.\n\n")
	for _, cat := range categories {
		count := len(categoryMap[cat])
		word := "commands"
		if count == 1 {
			word = "command"
		}
		sb.WriteString(fmt.Sprintf("**%s** - %s %s\n", cat, helpers.FormatNumber(count), word))
	}
	sb.WriteString(fmt.Sprintf("\nType `%shelp <command>` for detailed usage and examples.", prefix))

	fields := []*discordgo.MessageEmbedField{}
	if pol != nil {
		counts, _ := pol.GetDisabledCounts(guildID)
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   "Disabled Commands",
				Value:  helpers.FormatNumber(counts.Guild),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "Globally Disabled",
				Value:  helpers.FormatNumber(counts.Global),
				Inline: true,
			},
		)
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Help Menu (%s Commands)", helpers.FormatNumber(totalCmds)),
		Description: sb.String(),
		Fields:      fields,
		Color:       helpers.ColorDefault,
	}
}

func buildCategoryEmbeds(prefix, categoryName string, cmds []commands.Command) []*discordgo.MessageEmbed {
	var pages []*strings.Builder
	currentSb := &strings.Builder{}
	pages = append(pages, currentSb)

	appendLine := func(line string) {
		if currentSb.Len()+len(line) > 3200 {
			currentSb = &strings.Builder{}
			pages = append(pages, currentSb)
		}
		currentSb.WriteString(line)
	}

	for _, cmd := range cmds {
		cmdPrefix := prefix
		if _, ok := cmd.(commands.SlashOnly); ok {
			cmdPrefix = "/"
		}

		desc := cmd.Description()
		if desc == "" {
			desc = "No description provided."
		}
		appendLine(fmt.Sprintf("`%s%s` - %s\n", cmdPrefix, cmd.Name(), desc))

		if subProvider, ok := cmd.(commands.Subcommandable); ok {
			subList := subProvider.Subcommands()
			if len(subList) > 0 {
				sortedSubs := make([]commands.Subcommand, len(subList))
				copy(sortedSubs, subList)
				sort.Slice(sortedSubs, func(i, j int) bool {
					return sortedSubs[i].Name < sortedSubs[j].Name
				})

				for _, sub := range sortedSubs {
					subDesc := sub.Description
					if subDesc == "" {
						subDesc = "No description available"
					}
					appendLine(fmt.Sprintf("  > `%s%s %s` - %s\n", cmdPrefix, cmd.Name(), sub.Name, subDesc))
				}
			}
		}
	}

	var embeds []*discordgo.MessageEmbed
	totalPages := len(pages)

	for i, pageSb := range pages {
		pageText := strings.TrimSpace(pageSb.String())
		if pageText == "" {
			continue
		}

		title := fmt.Sprintf("%s Commands (%s)", categoryName, helpers.FormatNumber(len(cmds)))
		footerText := fmt.Sprintf("Use %shelp <command> for detailed usage and examples.", prefix)
		if totalPages > 1 {
			title = fmt.Sprintf("%s Commands (%s) · Page %d/%d", categoryName, helpers.FormatNumber(len(cmds)), i+1, totalPages)
			footerText = fmt.Sprintf("Use %shelp <command> for detailed usage and examples. Page %d of %d", prefix, i+1, totalPages)
		}

		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       title,
			Description: pageText,
			Color:       helpers.ColorDefault,
			Footer: &discordgo.MessageEmbedFooter{
				Text: footerText,
			},
		})
	}

	if len(embeds) == 0 {
		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("%s Commands", categoryName),
			Description: "No commands available in this category.",
		})
	}

	return embeds
}

type helpComponentsOptions struct {
	selectCustomID string
	prevCustomID   string
	nextCustomID   string
	categories     []string
	categoryMap    map[string][]commands.Command
	activeCategory string
	pageIndex      int
	totalPages     int
}

func buildHelpComponents(opts helpComponentsOptions) []discordgo.MessageComponent {
	options := []discordgo.SelectMenuOption{
		{
			Label:       "Main Menu",
			Value:       "HOME",
			Description: "Return to the main category overview",
			Default:     opts.activeCategory == "HOME" || opts.activeCategory == "",
		},
	}

	for _, cat := range opts.categories {
		options = append(options, discordgo.SelectMenuOption{
			Label:       cat,
			Value:       cat,
			Description: fmt.Sprintf("View %s %s commands", helpers.FormatNumber(len(opts.categoryMap[cat])), cat),
			Default:     cat == opts.activeCategory,
		})
	}

	rows := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    opts.selectCustomID,
					Placeholder: "Choose a category...",
					Options:     options,
				},
			},
		},
	}

	if opts.totalPages > 1 {
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "‹",
					Style:    discordgo.PrimaryButton,
					CustomID: opts.prevCustomID,
				},
				discordgo.Button{
					Label:    fmt.Sprintf("Page %d/%d", opts.pageIndex+1, opts.totalPages),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("help_info_%s", opts.selectCustomID),
					Disabled: true,
				},
				discordgo.Button{
					Label:    "›",
					Style:    discordgo.PrimaryButton,
					CustomID: opts.nextCustomID,
				},
			},
		})
	}

	return rows
}
