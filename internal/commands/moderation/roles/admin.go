package roles

import (
	"fmt"
	"regexp"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var customEmojiRegex = regexp.MustCompile(`<a?:[a-zA-Z0-9_]+:([0-9]+)>`)

func (c *RoleCmd) handleCreate(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "role create <name> [hexColor]") {
		return nil
	}

	subArgs := ctx.SubArgs()
	var colorInt int
	var roleName string

	lastArg := subArgs[len(subArgs)-1]
	parsedColor := helpers.ParseHexColor(lastArg)
	if parsedColor != 0 || strings.HasPrefix(lastArg, "#") || strings.HasPrefix(lastArg, "0x") {
		colorInt = parsedColor
		roleName = strings.Join(subArgs[:len(subArgs)-1], " ")
	} else {
		roleName = strings.Join(subArgs, " ")
	}

	if strings.TrimSpace(roleName) == "" {
		return fmt.Errorf("please provide a valid role name")
	}

	newRole, err := ctx.Session.GuildRoleCreate(ctx.Message.GuildID, &discordgo.RoleParams{
		Name:  roleName,
		Color: helpers.Ptr(colorInt),
	})
	if err != nil {
		return err
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Created role <@&%s> with color `#%06X`.", newRole.ID, newRole.Color),
		Color:       newRole.Color,
	})
	return err
}

func (c *RoleCmd) handleDelete(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "role delete <role>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	roleArg := strings.Join(subArgs, " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to permanently delete role **%s**?", targetRole.Name))
	if err != nil || !confirmed {
		return err
	}

	roleName := targetRole.Name
	err = ctx.Session.GuildRoleDelete(ctx.Message.GuildID, targetRole.ID)
	if err != nil {
		return err
	}

	return ctx.SendSuccess("Deleted role **%s**.", roleName)
}

func (c *RoleCmd) handleRename(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "role rename <role> <new_name>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	targetRole, err := ctx.ResolveRole(subArgs[0])
	newName := strings.Join(subArgs[1:], " ")

	if err != nil || targetRole == nil {
		for i := len(subArgs) - 1; i >= 1; i-- {
			possibleRole := strings.Join(subArgs[:i], " ")
			if r, errRes := ctx.ResolveRole(possibleRole); errRes == nil && r != nil {
				targetRole = r
				newName = strings.Join(subArgs[i:], " ")
				break
			}
		}
	}

	if targetRole == nil || strings.TrimSpace(newName) == "" {
		return fmt.Errorf("usage: `%srole rename <role> <new_name>`", ctx.Prefix)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	_, err = ctx.Session.GuildRoleEdit(ctx.Message.GuildID, targetRole.ID, &discordgo.RoleParams{
		Name: newName,
	})
	if err != nil {
		return err
	}

	return ctx.SendSuccess("Renamed role <@&%s> to **%s**.", targetRole.ID, newName)
}

func (c *RoleCmd) handleColor(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "role color <role> <hex>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	hexCode := subArgs[len(subArgs)-1]
	roleArg := strings.Join(subArgs[:len(subArgs)-1], " ")

	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	colorInt := helpers.ParseHexColor(hexCode)

	_, err = ctx.Session.GuildRoleEdit(ctx.Message.GuildID, targetRole.ID, &discordgo.RoleParams{
		Color: helpers.Ptr(colorInt),
	})
	if err != nil {
		return err
	}

	return ctx.SendSuccess("Updated color for <@&%s> to `#%06X`.", targetRole.ID, colorInt)
}

func (c *RoleCmd) handleIcon(ctx *bot.Context) error {
	subArgs := ctx.SubArgs()
	if len(subArgs) == 0 && len(ctx.Message.Attachments) == 0 {
		return fmt.Errorf("usage: `%srole icon <role> <emoji|URL|none>` or upload an attachment", ctx.Prefix)
	}

	var iconArg string
	var roleArg string

	if len(ctx.Message.Attachments) > 0 {
		iconArg = ctx.Message.Attachments[0].URL
		roleArg = strings.Join(subArgs, " ")
	} else if len(subArgs) >= 2 {
		iconArg = subArgs[len(subArgs)-1]
		roleArg = strings.Join(subArgs[:len(subArgs)-1], " ")
	} else {
		return fmt.Errorf("please specify a role and icon (or upload an attachment)")
	}

	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	roleParams := &discordgo.RoleParams{}

	cleanIcon := strings.TrimSpace(iconArg)
	switch strings.ToLower(cleanIcon) {
	case "none", "clear", "reset":
		emptyStr := ""
		roleParams.Icon = &emptyStr
		roleParams.UnicodeEmoji = &emptyStr
	default:
		if match := customEmojiRegex.FindStringSubmatch(cleanIcon); len(match) > 1 {
			emojiURL := fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.png", match[1])
			base64Data, err := helpers.FetchImageAsBase64(emojiURL)
			if err != nil {
				return fmt.Errorf("failed to fetch custom emoji icon: %w", err)
			}
			roleParams.Icon = &base64Data
		} else if helpers.IsURLSafe(cleanIcon) {
			base64Data, err := helpers.FetchImageAsBase64(cleanIcon)
			if err != nil {
				return fmt.Errorf("failed to process image icon: %w", err)
			}
			roleParams.Icon = &base64Data
		} else {
			roleParams.UnicodeEmoji = &cleanIcon
		}
	}

	_, err = ctx.Session.GuildRoleEdit(ctx.Message.GuildID, targetRole.ID, roleParams)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "400") || strings.Contains(strings.ToLower(err.Error()), "feature") {
			return fmt.Errorf("failed to update role icon. Note: Setting custom role icons requires Server Boost Level 2")
		}
		return err
	}

	return ctx.SendSuccess("Updated role icon for <@&%s>.", targetRole.ID)
}

func (c *RoleCmd) handleHoist(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "role hoist <role>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	roleArg := strings.Join(subArgs, " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	newHoist := !targetRole.Hoist
	_, err = ctx.Session.GuildRoleEdit(ctx.Message.GuildID, targetRole.ID, &discordgo.RoleParams{
		Hoist: &newHoist,
	})
	if err != nil {
		return err
	}

	status := "hoisted (displayed separately)"
	if !newHoist {
		status = "unhoisted"
	}
	return ctx.SendSuccess("Role <@&%s> is now **%s**.", targetRole.ID, status)
}

func (c *RoleCmd) handleMentionable(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "role mentionable <role>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	roleArg := strings.Join(subArgs, " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	newMention := !targetRole.Mentionable
	_, err = ctx.Session.GuildRoleEdit(ctx.Message.GuildID, targetRole.ID, &discordgo.RoleParams{
		Mentionable: &newMention,
	})
	if err != nil {
		return err
	}

	status := "now mentionable"
	if !newMention {
		status = "no longer mentionable"
	}
	return ctx.SendSuccess("Role <@&%s> is **%s**.", targetRole.ID, status)
}

func (c *RoleCmd) handleCopy(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "role copy <role> <new_name>") {
		return nil
	}

	subArgs := ctx.SubArgs()

	targetRole, err := ctx.ResolveRole(subArgs[0])
	newName := strings.Join(subArgs[1:], " ")

	if err != nil || targetRole == nil {
		for i := len(subArgs) - 1; i >= 1; i-- {
			possibleRole := strings.Join(subArgs[:i], " ")
			if r, errRes := ctx.ResolveRole(possibleRole); errRes == nil && r != nil {
				targetRole = r
				newName = strings.Join(subArgs[i:], " ")
				break
			}
		}
	}

	if targetRole == nil || strings.TrimSpace(newName) == "" {
		return fmt.Errorf("usage: `%srole copy <role> <new_name>`", ctx.Prefix)
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return err
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return err
	}

	perms := targetRole.Permissions
	if ctx.Message.Member != nil && ctx.Message.Member.Permissions&discordgo.PermissionAdministrator == 0 {
		perms = perms &^ int64(helpers.DangerousPermissions)
	}

	newRole, err := ctx.Session.GuildRoleCreate(ctx.Message.GuildID, &discordgo.RoleParams{
		Name:        newName,
		Color:       &targetRole.Color,
		Hoist:       &targetRole.Hoist,
		Mentionable: &targetRole.Mentionable,
		Permissions: &perms,
	})
	if err != nil {
		return err
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Copied settings from <@&%s> to new role <@&%s>.", targetRole.ID, newRole.ID),
		Color:       newRole.Color,
	})
	return err
}
