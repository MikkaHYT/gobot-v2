package database

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gobot/internal/helpers"
)

type GuildConfig struct {
	GuildID                        string   `json:"guild_id"`
	Prefix                         string   `json:"prefix"`
	EmbedColor                     string   `json:"embed_color"`
	ModLogChannelID                string   `json:"mod_log_channel_id"`
	StarboardChannelID             string   `json:"starboard_channel_id"`
	StarboardThreshold             int      `json:"starboard_threshold"`
	AutoroleIDs                    []string `json:"autorole_ids"`
	SelfReactDisabled              bool     `json:"self_react_disabled"`
	AntiMP3Mode                    string   `json:"antimp3_mode"`
	SnipeLimit                     int      `json:"snipe_limit"`
	RipMode                        string   `json:"rip_mode"`
	RadioRestrictionMode           string   `json:"radio_restriction_mode"`
	RadioAutojoinChannelID         string   `json:"radio_autojoin_channel_id"`
	RadioPlayerChannelID           string   `json:"radio_player_channel_id"`
	LevelUpMessagesEnabled         bool     `json:"levelup_messages_enabled"`
	JailRoleID                     string   `json:"jail_role_id"`
	JailChannelID                  string   `json:"jail_channel_id"`
	MuteRoleID                     string   `json:"mute_role_id"`
	ImageMuteRoleID                string   `json:"imagemute_role_id"`
	ReactionMuteRoleID             string   `json:"reactionmute_role_id"`
	BlacklistedChannelIDs          []string `json:"blacklisted_channels"`
	BlacklistedRoleIDs             []string `json:"blacklisted_roles"`
	BlacklistedUserIDs             []string `json:"blacklisted_users"`
	VoiceMasterTriggerChannelID    string   `json:"voicemaster_trigger_channel_id"`
	VoiceMasterCategoryID          string   `json:"voicemaster_category_id"`
	InviteFilterEnabled            bool     `json:"invite_filter_enabled"`
	InviteFilterPunishment         string   `json:"invite_filter_punishment"`
	InviteFilterWhitelistedRoleIDs []string `json:"invite_filter_whitelisted_roles"`
	WordFilterWhitelistedRoleIDs   []string `json:"word_filter_whitelisted_roles"`
	WordFilterPunishment           string   `json:"word_filter_punishment"`
	XPMultiplier                   float64  `json:"xp_multiplier"`
	IgnoredLevelChannelIDs         []string `json:"ignored_level_channel_ids"`
	LevelRolesStack                bool     `json:"level_roles_stack"`
	LockedChannelIDs               []string `json:"locked_channels"`
	WelcomeChannelID               string   `json:"welcome_channel_id"`
	WelcomeMessage                 string   `json:"welcome_message"`
	WelcomeEmbedJSON               string   `json:"welcome_embed_json"`
	WelcomeIsEmbed                 bool     `json:"welcome_is_embed"`
	WelcomeDMEnabled               bool     `json:"welcome_dm_enabled"`
	WelcomeDMMessage               string   `json:"welcome_dm_message"`
	WelcomeDMEmbedJSON             string   `json:"welcome_dm_embed_json"`
	WelcomeDMIsEmbed               bool     `json:"welcome_dm_is_embed"`
	GoodbyeChannelID               string   `json:"goodbye_channel_id"`
	GoodbyeMessage                 string   `json:"goodbye_message"`
	GoodbyeEmbedJSON               string   `json:"goodbye_embed_json"`
	GoodbyeIsEmbed                 bool     `json:"goodbye_is_embed"`
}

func (c *GuildConfig) Clone() *GuildConfig {
	if c == nil {
		return nil
	}
	clone := *c

	clone.AutoroleIDs = slices.Clone(c.AutoroleIDs)
	clone.BlacklistedChannelIDs = slices.Clone(c.BlacklistedChannelIDs)
	clone.BlacklistedRoleIDs = slices.Clone(c.BlacklistedRoleIDs)
	clone.BlacklistedUserIDs = slices.Clone(c.BlacklistedUserIDs)
	clone.InviteFilterWhitelistedRoleIDs = slices.Clone(c.InviteFilterWhitelistedRoleIDs)
	clone.WordFilterWhitelistedRoleIDs = slices.Clone(c.WordFilterWhitelistedRoleIDs)
	clone.IgnoredLevelChannelIDs = slices.Clone(c.IgnoredLevelChannelIDs)
	clone.LockedChannelIDs = slices.Clone(c.LockedChannelIDs)

	return &clone
}

type GuildSetting string

const (
	SettingPrefix                       GuildSetting = "prefix"
	SettingEmbedColor                   GuildSetting = "embed_color"
	SettingModLogChannelID              GuildSetting = "mod_log_channel_id"
	SettingStarboardChannelID           GuildSetting = "starboard_channel_id"
	SettingStarboardThreshold           GuildSetting = "starboard_threshold"
	SettingAutoroleIDs                  GuildSetting = "autoroles_json"
	SettingSelfReactDisabled            GuildSetting = "self_react_disabled"
	SettingAntiMP3Mode                  GuildSetting = "antimp3_mode"
	SettingSnipeLimit                   GuildSetting = "snipe_limit"
	SettingRipMode                      GuildSetting = "rip_mode"
	SettingRadioRestrictionMode         GuildSetting = "radio_restriction_mode"
	SettingRadioAutojoinChannelID       GuildSetting = "radio_autojoin_channel_id"
	SettingRadioPlayerChannelID         GuildSetting = "radio_player_channel_id"
	SettingLevelUpMessagesEnabled       GuildSetting = "levelup_messages_enabled"
	SettingJailRoleID                   GuildSetting = "jail_role_id"
	SettingJailChannelID                GuildSetting = "jail_channel_id"
	SettingMuteRoleID                   GuildSetting = "mute_role_id"
	SettingBlacklistedChannelIDs        GuildSetting = "blacklisted_channels"
	SettingBlacklistedRoleIDs           GuildSetting = "blacklisted_roles"
	SettingBlacklistedUserIDs           GuildSetting = "blacklisted_users"
	SettingVoiceMasterTriggerChannelID  GuildSetting = "voicemaster_trigger_channel_id"
	SettingVoiceMasterCategoryID        GuildSetting = "voicemaster_category_id"
	SettingImageMuteRoleID              GuildSetting = "imagemute_role_id"
	SettingReactionMuteRoleID           GuildSetting = "reactionmute_role_id"
	SettingInviteFilterEnabled          GuildSetting = "invite_filter_enabled"
	SettingInviteFilterPunishment       GuildSetting = "invite_filter_punishment"
	SettingInviteFilterWhitelistedRoles GuildSetting = "invite_filter_whitelisted_roles"
	SettingWordFilterWhitelistedRoles   GuildSetting = "word_filter_whitelisted_roles"
	SettingWordFilterPunishment         GuildSetting = "word_filter_punishment"
	SettingXPMultiplier                 GuildSetting = "xp_multiplier"
	SettingIgnoredLevelChannelIDs       GuildSetting = "ignored_level_channel_ids"
	SettingLevelRolesStack              GuildSetting = "level_roles_stack"
	SettingLockedChannels               GuildSetting = "locked_channels"
	SettingWelcomeChannelID             GuildSetting = "welcome_channel_id"
	SettingWelcomeMessage               GuildSetting = "welcome_message"
	SettingWelcomeEmbedJSON             GuildSetting = "welcome_embed_json"
	SettingWelcomeIsEmbed               GuildSetting = "welcome_is_embed"
	SettingWelcomeDMEnabled             GuildSetting = "welcome_dm_enabled"
	SettingWelcomeDMMessage             GuildSetting = "welcome_dm_message"
	SettingWelcomeDMEmbedJSON           GuildSetting = "welcome_dm_embed_json"
	SettingWelcomeDMIsEmbed             GuildSetting = "welcome_dm_is_embed"
	SettingGoodbyeChannelID             GuildSetting = "goodbye_channel_id"
	SettingGoodbyeMessage               GuildSetting = "goodbye_message"
	SettingGoodbyeEmbedJSON             GuildSetting = "goodbye_embed_json"
	SettingGoodbyeIsEmbed               GuildSetting = "goodbye_is_embed"
)

const (
	RadioRestrictionModeAll          = "all"
	RadioRestrictionModeVoiceChannel = "vc"
	RadioRestrictionModeModerators   = "mods"
)

func NormalizeRadioRestrictionMode(mode string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(mode)); normalized {
	case "", "none":
		return RadioRestrictionModeAll
	case "dj_only":
		return RadioRestrictionModeVoiceChannel
	case "admin_only":
		return RadioRestrictionModeModerators
	default:
		return normalized
	}
}

var validGuildColumns = map[GuildSetting]string{
	SettingLevelUpMessagesEnabled:       "levelup_messages_enabled",
	SettingImageMuteRoleID:              "imagemute_role_id",
	SettingReactionMuteRoleID:           "reactionmute_role_id",
	SettingPrefix:                       "prefix",
	SettingEmbedColor:                   "embed_color",
	SettingModLogChannelID:              "mod_log_channel_id",
	SettingStarboardChannelID:           "starboard_channel_id",
	SettingStarboardThreshold:           "starboard_threshold",
	SettingAutoroleIDs:                  "autoroles_json",
	SettingSelfReactDisabled:            "self_react_disabled",
	SettingAntiMP3Mode:                  "antimp3_mode",
	SettingSnipeLimit:                   "snipe_limit",
	SettingRipMode:                      "rip_mode",
	SettingRadioRestrictionMode:         "radio_restriction_mode",
	SettingRadioAutojoinChannelID:       "radio_autojoin_channel_id",
	SettingRadioPlayerChannelID:         "radio_player_channel_id",
	SettingJailRoleID:                   "jail_role_id",
	SettingJailChannelID:                "jail_channel_id",
	SettingMuteRoleID:                   "mute_role_id",
	SettingBlacklistedChannelIDs:        "blacklisted_channels",
	SettingBlacklistedRoleIDs:           "blacklisted_roles",
	SettingBlacklistedUserIDs:           "blacklisted_users",
	SettingVoiceMasterTriggerChannelID:  "voicemaster_trigger_channel_id",
	SettingVoiceMasterCategoryID:        "voicemaster_category_id",
	SettingInviteFilterEnabled:          "invite_filter_enabled",
	SettingInviteFilterPunishment:       "invite_filter_punishment",
	SettingInviteFilterWhitelistedRoles: "invite_filter_whitelisted_roles",
	SettingWordFilterWhitelistedRoles:   "word_filter_whitelisted_roles",
	SettingWordFilterPunishment:         "word_filter_punishment",
	SettingXPMultiplier:                 "xp_multiplier",
	SettingIgnoredLevelChannelIDs:       "ignored_level_channel_ids",
	SettingLevelRolesStack:              "level_roles_stack",
	SettingLockedChannels:               "locked_channels",
	SettingWelcomeChannelID:             "welcome_channel_id",
	SettingWelcomeMessage:               "welcome_message",
	SettingWelcomeEmbedJSON:             "welcome_embed_json",
	SettingWelcomeIsEmbed:               "welcome_is_embed",
	SettingWelcomeDMEnabled:             "welcome_dm_enabled",
	SettingWelcomeDMMessage:             "welcome_dm_message",
	SettingWelcomeDMEmbedJSON:           "welcome_dm_embed_json",
	SettingWelcomeDMIsEmbed:             "welcome_dm_is_embed",
	SettingGoodbyeChannelID:             "goodbye_channel_id",
	SettingGoodbyeMessage:               "goodbye_message",
	SettingGoodbyeEmbedJSON:             "goodbye_embed_json",
	SettingGoodbyeIsEmbed:               "goodbye_is_embed",
}

func (d *DB) GetGuildConfig(guildID string) (*GuildConfig, error) {
	if d.Cache != nil {
		if cached, ok := d.Cache.Get(guildID); ok && cached != nil {
			return cached.Clone(), nil
		}
	}

	ctx, cancel := newCtx()
	defer cancel()

	cfg := &GuildConfig{
		GuildID:                        guildID,
		Prefix:                         "",
		EmbedColor:                     "",
		ModLogChannelID:                "",
		StarboardChannelID:             "",
		StarboardThreshold:             3,
		AutoroleIDs:                    make([]string, 0),
		SelfReactDisabled:              false,
		AntiMP3Mode:                    "disable",
		SnipeLimit:                     25,
		RipMode:                        "transform",
		RadioRestrictionMode:           RadioRestrictionModeAll,
		RadioAutojoinChannelID:         "",
		LevelUpMessagesEnabled:         false,
		JailRoleID:                     "",
		JailChannelID:                  "",
		MuteRoleID:                     "",
		ImageMuteRoleID:                "",
		ReactionMuteRoleID:             "",
		BlacklistedChannelIDs:          make([]string, 0),
		BlacklistedRoleIDs:             make([]string, 0),
		BlacklistedUserIDs:             make([]string, 0),
		VoiceMasterTriggerChannelID:    "",
		VoiceMasterCategoryID:          "",
		InviteFilterEnabled:            false,
		InviteFilterPunishment:         "delete",
		InviteFilterWhitelistedRoleIDs: make([]string, 0),
		WordFilterWhitelistedRoleIDs:   make([]string, 0),
		WordFilterPunishment:           "delete",
		XPMultiplier:                   1.0,
		IgnoredLevelChannelIDs:         make([]string, 0),
		LevelRolesStack:                true,
		LockedChannelIDs:               make([]string, 0),
		WelcomeChannelID:               "",
		WelcomeMessage:                 "",
		WelcomeEmbedJSON:               "",
		WelcomeIsEmbed:                 false,
		WelcomeDMEnabled:               false,
		WelcomeDMMessage:               "",
		WelcomeDMEmbedJSON:             "",
		WelcomeDMIsEmbed:               false,
		GoodbyeChannelID:               "",
		GoodbyeMessage:                 "",
		GoodbyeEmbedJSON:               "",
		GoodbyeIsEmbed:                 false,
	}

	var autorolesJSON, blChannelsJSON, blRolesJSON, blUsersJSON string
	var inviteRolesJSON, wordRolesJSON, ignoredLevelJSON, lockedJSON string
	var inviteFilterEnabledStr, xpMultiplierStr, levelRolesStackStr string

	query := `SELECT prefix, embed_color, mod_log_channel_id, starboard_channel_id, starboard_threshold,
	          autoroles_json, self_react_disabled, antimp3_mode, snipe_limit, rip_mode,
	          radio_restriction_mode, radio_autojoin_channel_id, radio_player_channel_id, levelup_messages_enabled,
	          jail_role_id, jail_channel_id, mute_role_id, imagemute_role_id, reactionmute_role_id,
	          blacklisted_channels, blacklisted_roles, blacklisted_users,
	          voicemaster_trigger_channel_id, voicemaster_category_id,
	          invite_filter_enabled, invite_filter_punishment, invite_filter_whitelisted_roles,
	          word_filter_whitelisted_roles, word_filter_punishment, xp_multiplier,
	          ignored_level_channel_ids, level_roles_stack, locked_channels,
	          welcome_channel_id, welcome_message, welcome_embed_json, welcome_is_embed,
	          welcome_dm_enabled, welcome_dm_message, welcome_dm_embed_json, welcome_dm_is_embed,
	          goodbye_channel_id, goodbye_message, goodbye_embed_json, goodbye_is_embed
	          FROM guild_configs WHERE guild_id = ?`

	err := d.conn.QueryRowContext(ctx, query, guildID).Scan(
		&cfg.Prefix,
		&cfg.EmbedColor,
		&cfg.ModLogChannelID,
		&cfg.StarboardChannelID,
		&cfg.StarboardThreshold,
		&autorolesJSON,
		&cfg.SelfReactDisabled,
		&cfg.AntiMP3Mode,
		&cfg.SnipeLimit,
		&cfg.RipMode,
		&cfg.RadioRestrictionMode,
		&cfg.RadioAutojoinChannelID,
		&cfg.RadioPlayerChannelID,
		&cfg.LevelUpMessagesEnabled,
		&cfg.JailRoleID,
		&cfg.JailChannelID,
		&cfg.MuteRoleID,
		&cfg.ImageMuteRoleID,
		&cfg.ReactionMuteRoleID,
		&blChannelsJSON,
		&blRolesJSON,
		&blUsersJSON,
		&cfg.VoiceMasterTriggerChannelID,
		&cfg.VoiceMasterCategoryID,
		&inviteFilterEnabledStr,
		&cfg.InviteFilterPunishment,
		&inviteRolesJSON,
		&wordRolesJSON,
		&cfg.WordFilterPunishment,
		&xpMultiplierStr,
		&ignoredLevelJSON,
		&levelRolesStackStr,
		&lockedJSON,
		&cfg.WelcomeChannelID,
		&cfg.WelcomeMessage,
		&cfg.WelcomeEmbedJSON,
		&cfg.WelcomeIsEmbed,
		&cfg.WelcomeDMEnabled,
		&cfg.WelcomeDMMessage,
		&cfg.WelcomeDMEmbedJSON,
		&cfg.WelcomeDMIsEmbed,
		&cfg.GoodbyeChannelID,
		&cfg.GoodbyeMessage,
		&cfg.GoodbyeEmbedJSON,
		&cfg.GoodbyeIsEmbed,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query guild_config: %w", err)
	}
	cfg.RadioRestrictionMode = NormalizeRadioRestrictionMode(cfg.RadioRestrictionMode)

	if val, ok := helpers.ParseBool(inviteFilterEnabledStr); ok {
		cfg.InviteFilterEnabled = val
	}
	if levelRolesStackStr == "" {
		cfg.LevelRolesStack = true
	} else if val, ok := helpers.ParseBool(levelRolesStackStr); ok {
		cfg.LevelRolesStack = val
	}
	if xpMultiplierStr != "" {
		if val, parseErr := fmt.Sscanf(xpMultiplierStr, "%f", &cfg.XPMultiplier); parseErr != nil || val <= 0 {
			cfg.XPMultiplier = 1.0
		}
	}

	if err := unmarshalStringSlice(autorolesJSON, "autoroles", guildID, &cfg.AutoroleIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(blChannelsJSON, "blacklisted_channels", guildID, &cfg.BlacklistedChannelIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(blRolesJSON, "blacklisted_roles", guildID, &cfg.BlacklistedRoleIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(blUsersJSON, "blacklisted_users", guildID, &cfg.BlacklistedUserIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(inviteRolesJSON, "invite_filter_whitelisted_roles", guildID, &cfg.InviteFilterWhitelistedRoleIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(wordRolesJSON, "word_filter_whitelisted_roles", guildID, &cfg.WordFilterWhitelistedRoleIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(ignoredLevelJSON, "ignored_level_channel_ids", guildID, &cfg.IgnoredLevelChannelIDs); err != nil {
		return nil, err
	}
	if err := unmarshalStringSlice(lockedJSON, "locked_channels", guildID, &cfg.LockedChannelIDs); err != nil {
		return nil, err
	}

	if d.Cache != nil {
		d.Cache.Set(guildID, cfg.Clone())
	}

	return cfg.Clone(), nil
}
func (d *DB) DeleteGuildConfig(guildID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM guild_configs WHERE guild_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID); err != nil {
		return fmt.Errorf("failed to delete guild_config for %s: %w", guildID, err)
	}

	if d.Cache != nil {
		d.Cache.Invalidate(guildID)
	}

	return nil
}

func (d *DB) UpdateGuildSetting(guildID string, key GuildSetting, value any) error {
	return d.UpdateGuildSettings(guildID, map[GuildSetting]any{key: value})
}

func (d *DB) UpdateGuildSettings(guildID string, updates map[GuildSetting]any) error {
	if guildID == "" {
		return errors.New("guildID cannot be empty")
	}
	if len(updates) == 0 {
		return nil
	}

	ctx, cancel := newCtx()
	defer cancel()

	cols := make([]string, 0, len(updates)+1)
	placeholders := make([]string, 0, len(updates)+1)
	updateClauses := make([]string, 0, len(updates))
	args := make([]any, 0, len(updates)+1)

	cols = append(cols, "guild_id")
	placeholders = append(placeholders, "?")
	args = append(args, guildID)

	for key, val := range updates {
		colName, ok := validGuildColumns[key]
		if !ok {
			return fmt.Errorf("invalid guild setting key: %s", key)
		}

		switch key {
		case SettingPrefix:
			strVal := strings.TrimSpace(fmt.Sprintf("%v", val))
			if strVal == "" {
				return fmt.Errorf("prefix cannot be empty or whitespace only")
			}
			if len(strVal) > 5 {
				return fmt.Errorf("prefix cannot exceed 5 characters")
			}
			val = strVal
		case SettingStarboardThreshold:
			intVal, ok := val.(int)
			if !ok {
				if i, err := strconv.Atoi(fmt.Sprintf("%v", val)); err == nil {
					intVal = i
				} else {
					return fmt.Errorf("invalid starboard threshold: %v", val)
				}
			}
			if intVal < 1 || intVal > 100 {
				return fmt.Errorf("starboard threshold must be between 1 and 100")
			}
			val = intVal
		case SettingSnipeLimit:
			intVal, ok := val.(int)
			if !ok {
				if i, err := strconv.Atoi(fmt.Sprintf("%v", val)); err == nil {
					intVal = i
				} else {
					return fmt.Errorf("invalid snipe limit: %v", val)
				}
			}
			if intVal < 1 || intVal > 1000 {
				return fmt.Errorf("snipe limit must be between 1 and 1000")
			}
			val = intVal
		case SettingXPMultiplier:
			strVal := fmt.Sprintf("%v", val)
			f, err := strconv.ParseFloat(strVal, 64)
			if err != nil || f <= 0 || f > 100.0 {
				return fmt.Errorf("xp multiplier must be between 0.01 and 100.0")
			}
			val = fmt.Sprintf("%.2f", f)
		case SettingRadioRestrictionMode:
			val = NormalizeRadioRestrictionMode(fmt.Sprintf("%v", val))
		case SettingInviteFilterPunishment, SettingWordFilterPunishment:
			p := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", val)))
			switch p {
			case "delete", "timeout", "kick", "ban", "none", "":
				val = p
			default:
				return fmt.Errorf("invalid punishment %q; must be delete, timeout, kick, ban, or none", p)
			}
		case SettingEmbedColor:
			if intVal, ok := val.(int); ok {
				if intVal < 0 || intVal > 0xFFFFFF {
					return fmt.Errorf("embed color must be between 0x000000 and 0xFFFFFF")
				}
			}
		}

		finalVal := val
		switch v := val.(type) {
		case []string:
			b, err := marshalStringSlice(v, string(key))
			if err != nil {
				return err
			}
			finalVal = string(b)
		}

		cols = append(cols, colName)
		placeholders = append(placeholders, "?")
		updateClauses = append(updateClauses, fmt.Sprintf("%s = excluded.%s", colName, colName))
		args = append(args, finalVal)
	}

	query := fmt.Sprintf(
		"INSERT INTO guild_configs (%s) VALUES (%s) ON CONFLICT(guild_id) DO UPDATE SET %s;",
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(updateClauses, ", "),
	)

	if _, err := d.conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to update guild settings for %s: %w", guildID, err)
	}

	if d.Cache != nil {
		d.Cache.Invalidate(guildID)
	}

	return nil
}

func (d *DB) GetGuildSetting(guildID string, key GuildSetting) (any, error) {
	colName, ok := validGuildColumns[key]
	if !ok {
		return nil, fmt.Errorf("invalid guild setting key: %s", key)
	}

	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf("SELECT %s FROM guild_configs WHERE guild_id = ?", colName)

	switch key {
	case SettingStarboardThreshold, SettingSnipeLimit:
		var val int
		err := d.conn.QueryRowContext(ctx, query, guildID).Scan(&val)
		if errors.Is(err, sql.ErrNoRows) {
			if key == SettingStarboardThreshold {
				return 3, nil
			}
			if key == SettingSnipeLimit {
				return 25, nil
			}
			return 0, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query setting %s for guild %s: %w", key, guildID, err)
		}
		return val, nil

	case SettingSelfReactDisabled, SettingLevelUpMessagesEnabled, SettingInviteFilterEnabled:
		var val bool
		err := d.conn.QueryRowContext(ctx, query, guildID).Scan(&val)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query setting %s for guild %s: %w", key, guildID, err)
		}
		return val, nil

	case SettingAutoroleIDs, SettingBlacklistedChannelIDs, SettingBlacklistedRoleIDs, SettingBlacklistedUserIDs,
		SettingInviteFilterWhitelistedRoles, SettingWordFilterWhitelistedRoles, SettingIgnoredLevelChannelIDs, SettingLockedChannels:
		var jsonStr string
		err := d.conn.QueryRowContext(ctx, query, guildID).Scan(&jsonStr)
		if errors.Is(err, sql.ErrNoRows) {
			return make([]string, 0), nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query setting %s for guild %s: %w", key, guildID, err)
		}
		var slice []string
		if err := unmarshalStringSlice(jsonStr, string(key), guildID, &slice); err != nil {
			return nil, err
		}
		return slice, nil

	default:
		var val string
		err := d.conn.QueryRowContext(ctx, query, guildID).Scan(&val)
		if errors.Is(err, sql.ErrNoRows) {
			if key == SettingAntiMP3Mode {
				return "disable", nil
			}
			if key == SettingRipMode {
				return "transform", nil
			}
			if key == SettingRadioRestrictionMode {
				return RadioRestrictionModeAll, nil
			}
			return "", nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query setting %s for guild %s: %w", key, guildID, err)
		}
		if val == "" {
			if key == SettingRipMode {
				return "transform", nil
			}
			if key == SettingAntiMP3Mode {
				return "disable", nil
			}
			if key == SettingRadioRestrictionMode {
				return RadioRestrictionModeAll, nil
			}
		}
		if key == SettingRadioRestrictionMode {
			return NormalizeRadioRestrictionMode(val), nil
		}
		return val, nil
	}
}

func (d *DB) GetGuildSettingString(guildID string, key GuildSetting) (string, error) {
	val, err := d.GetGuildSetting(guildID, key)
	if err != nil {
		return "", err
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("setting %s is not a string (type %T)", key, val)
	}
	return s, nil
}

func (d *DB) GetGuildSettingInt(guildID string, key GuildSetting) (int, error) {
	val, err := d.GetGuildSetting(guildID, key)
	if err != nil {
		return 0, err
	}
	i, ok := val.(int)
	if !ok {
		return 0, fmt.Errorf("setting %s is not an int (type %T)", key, val)
	}
	return i, nil
}

func (d *DB) GetGuildSettingBool(guildID string, key GuildSetting) (bool, error) {
	val, err := d.GetGuildSetting(guildID, key)
	if err != nil {
		return false, err
	}
	b, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("setting %s is not a bool (type %T)", key, val)
	}
	return b, nil
}

func (d *DB) GetGuildSettingSlice(guildID string, key GuildSetting) ([]string, error) {
	val, err := d.GetGuildSetting(guildID, key)
	if err != nil {
		return nil, err
	}
	slice, ok := val.([]string)
	if !ok {
		return nil, fmt.Errorf("setting %s is not a []string (type %T)", key, val)
	}
	return slice, nil
}

func (d *DB) DisableCommand(guildID, commandName, disabledBy string) error {
	ctx, cancel := newCtx()
	defer cancel()

	if guildID == "" {
		guildID = "global"
	}

	query := `
	INSERT INTO disabled_commands (guild_id, command_name, disabled_by) VALUES (?, ?, ?)
	ON CONFLICT(guild_id, command_name) DO UPDATE SET disabled_by = excluded.disabled_by;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, commandName, disabledBy); err != nil {
		return fmt.Errorf("failed to disable command %q in %s: %w", commandName, guildID, err)
	}

	return nil
}

func (d *DB) EnableCommand(guildID, commandName string) error {
	ctx, cancel := newCtx()
	defer cancel()

	if guildID == "" {
		guildID = "global"
	}

	query := `DELETE FROM disabled_commands WHERE guild_id = ? AND command_name = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, commandName); err != nil {
		return fmt.Errorf("failed to enable command %q in %s: %w", commandName, guildID, err)
	}

	return nil
}

func (d *DB) IsCommandDisabled(guildID, commandName string) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT 1 FROM disabled_commands WHERE (guild_id = 'global' OR guild_id = ?) AND command_name = ?`
	var exists int
	err := d.conn.QueryRowContext(ctx, query, guildID, commandName).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check disabled command %q: %w", commandName, err)
	}
	return true, nil
}

func (d *DB) GetDisabledCommands(guildID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT command_name FROM disabled_commands WHERE guild_id = 'global' OR guild_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query disabled commands: %w", err)
	}
	defer rows.Close()

	var cmds []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan disabled command: %w", err)
		}
		cmds = append(cmds, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating disabled commands: %w", err)
	}
	return cmds, nil
}

func (d *DB) GetDisabledCommandCounts(guildID string) (int, int, error) {
	ctx, cancel := newCtx()
	defer cancel()

	var guildCount, globalCount int
	rowGuild := d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM disabled_commands WHERE guild_id = ?`, guildID)
	if err := rowGuild.Scan(&guildCount); err != nil {
		return 0, 0, fmt.Errorf("failed to scan guild disabled count: %w", err)
	}

	rowGlobal := d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM disabled_commands WHERE guild_id = 'global'`)
	if err := rowGlobal.Scan(&globalCount); err != nil {
		return 0, 0, fmt.Errorf("failed to scan global disabled count: %w", err)
	}

	return guildCount, globalCount, nil
}

type GlobalBlacklistEntry struct {
	TargetID   string    `json:"target_id"`
	TargetType string    `json:"target_type"`
	Reason     string    `json:"reason"`
	AddedBy    string    `json:"added_by"`
	CreatedAt  time.Time `json:"created_at"`
}

func (d *DB) AddGlobalBlacklist(targetID, targetType, reason, addedBy string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO global_blacklists (target_id, target_type, reason, added_by) VALUES (?, ?, ?, ?)
	ON CONFLICT(target_id) DO UPDATE SET target_type = excluded.target_type, reason = excluded.reason, added_by = excluded.added_by;`
	if _, err := d.conn.ExecContext(ctx, query, targetID, targetType, reason, addedBy); err != nil {
		return fmt.Errorf("failed to add global blacklist for %s: %w", targetID, err)
	}
	return nil
}

func (d *DB) RemoveGlobalBlacklist(targetID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM global_blacklists WHERE target_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, targetID); err != nil {
		return fmt.Errorf("failed to remove global blacklist for %s: %w", targetID, err)
	}
	return nil
}

func (d *DB) IsGlobalBlacklisted(targetID string) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT 1 FROM global_blacklists WHERE target_id = ?`
	var exists int
	err := d.conn.QueryRowContext(ctx, query, targetID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check global blacklist for %s: %w", targetID, err)
	}
	return true, nil
}

func (d *DB) GetAllGlobalBlacklists() ([]GlobalBlacklistEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT target_id, target_type, reason, added_by, created_at FROM global_blacklists ORDER BY created_at DESC`
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query global blacklists: %w", err)
	}
	defer rows.Close()

	var entries []GlobalBlacklistEntry
	for rows.Next() {
		var e GlobalBlacklistEntry
		var createdStr string
		if err := rows.Scan(&e.TargetID, &e.TargetType, &e.Reason, &e.AddedBy, &createdStr); err != nil {
			return nil, fmt.Errorf("failed to scan global blacklist entry: %w", err)
		}
		e.CreatedAt = parseTimestamp(createdStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating global blacklists: %w", err)
	}
	return entries, nil
}

func (d *DB) GetGlobalBlacklistsByType(targetType string) ([]GlobalBlacklistEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT target_id, target_type, reason, added_by, created_at FROM global_blacklists WHERE target_type = ? ORDER BY created_at DESC`
	rows, err := d.conn.QueryContext(ctx, query, targetType)
	if err != nil {
		return nil, fmt.Errorf("failed to query global blacklists for type %s: %w", targetType, err)
	}
	defer rows.Close()

	var entries []GlobalBlacklistEntry
	for rows.Next() {
		var e GlobalBlacklistEntry
		var createdStr string
		if err := rows.Scan(&e.TargetID, &e.TargetType, &e.Reason, &e.AddedBy, &createdStr); err != nil {
			return nil, fmt.Errorf("failed to scan global blacklist entry: %w", err)
		}
		e.CreatedAt = parseTimestamp(createdStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating global blacklists: %w", err)
	}
	return entries, nil
}
