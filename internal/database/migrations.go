package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const CurrentSchemaVersion = 1

type migrationStep struct {
	version int
	name    string
	up      func(ctx context.Context, tx *sql.Tx) error
}

var migrations = []migrationStep{
	{
		version: 1,
		name:    "base_schema_and_indices",
		up: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, baseSchemaSQL); err != nil {
				return fmt.Errorf("failed executing base schema: %w", err)
			}
			return nil
		},
	},
}

func runMigrations(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := db.ExecContext(ctx, baseSchemaSQL); err != nil {
		return fmt.Errorf("failed ensuring base schema tables exist: %w", err)
	}

	var currentVersion int
	row := db.QueryRowContext(ctx, "PRAGMA user_version")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to query database schema version: %w", err)
	}

	for _, m := range migrations {
		if currentVersion < m.version {
			if err := runSingleMigration(ctx, db, m); err != nil {
				return fmt.Errorf("migration v%d (%s) failed: %w", m.version, m.name, err)
			}
			currentVersion = m.version
		}
	}

	return nil
}

func runSingleMigration(parentCtx context.Context, db *sql.DB, m migrationStep) error {
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("failed starting migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := m.up(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("failed setting user_version to %d in transaction: %w", m.version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed committing migration v%d: %w", m.version, err)
	}

	return nil
}

const baseSchemaSQL = `
	CREATE TABLE IF NOT EXISTS guild_configs (
		guild_id TEXT PRIMARY KEY,
		prefix TEXT DEFAULT '',
		embed_color TEXT DEFAULT '',
		mod_log_channel_id TEXT DEFAULT '',
		starboard_channel_id TEXT DEFAULT '',
		starboard_threshold INTEGER DEFAULT 3,
		autoroles_json TEXT DEFAULT '[]',
		self_react_disabled BOOLEAN DEFAULT 0,
		antimp3_mode TEXT DEFAULT 'disable',
		snipe_limit INTEGER DEFAULT 25,
		rip_mode TEXT DEFAULT 'transform',
		radio_restriction_mode TEXT DEFAULT 'all',
		radio_autojoin_channel_id TEXT DEFAULT '',
		radio_player_channel_id TEXT DEFAULT '',
		levelup_messages_enabled BOOLEAN DEFAULT 0,
		jail_role_id TEXT DEFAULT '',
		jail_channel_id TEXT DEFAULT '',
		mute_role_id TEXT DEFAULT '',
		imagemute_role_id TEXT DEFAULT '',
		reactionmute_role_id TEXT DEFAULT '',
		blacklisted_channels TEXT DEFAULT '[]',
		blacklisted_roles TEXT DEFAULT '[]',
		blacklisted_users TEXT DEFAULT '[]',
		voicemaster_trigger_channel_id TEXT DEFAULT '',
		voicemaster_category_id TEXT DEFAULT '',
		invite_filter_enabled TEXT DEFAULT '0',
		invite_filter_punishment TEXT DEFAULT 'delete',
		invite_filter_whitelisted_roles TEXT DEFAULT '[]',
		word_filter_whitelisted_roles TEXT DEFAULT '[]',
		word_filter_punishment TEXT DEFAULT 'delete',
		xp_multiplier TEXT DEFAULT '1.0',
		ignored_level_channel_ids TEXT DEFAULT '[]',
		level_roles_stack TEXT DEFAULT 'true',
		locked_channels TEXT DEFAULT '[]',
		welcome_channel_id TEXT DEFAULT '',
		welcome_message TEXT DEFAULT '',
		welcome_embed_json TEXT DEFAULT '',
		welcome_is_embed BOOLEAN DEFAULT 0,
		welcome_dm_enabled BOOLEAN DEFAULT 0,
		welcome_dm_message TEXT DEFAULT '',
		welcome_dm_embed_json TEXT DEFAULT '',
		welcome_dm_is_embed BOOLEAN DEFAULT 0,
		goodbye_channel_id TEXT DEFAULT '',
		goodbye_message TEXT DEFAULT '',
		goodbye_embed_json TEXT DEFAULT '',
		goodbye_is_embed BOOLEAN DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS disabled_commands (
		guild_id TEXT DEFAULT 'global',
		command_name TEXT NOT NULL,
		disabled_by TEXT DEFAULT '',
		PRIMARY KEY(guild_id, command_name)
	);
	CREATE TABLE IF NOT EXISTS global_blacklists (
		target_id TEXT PRIMARY KEY,
		target_type TEXT NOT NULL,
		reason TEXT DEFAULT 'Blacklisted by bot owner',
		added_by TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS moderation_cases (
		case_id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		mod_id TEXT NOT NULL,
		action TEXT NOT NULL,
		reason TEXT DEFAULT 'No reason provided',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS jailed_users (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		roles_json TEXT NOT NULL,
		jailed_by TEXT NOT NULL,
		reason TEXT DEFAULT 'No reason provided',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id)
	);
	CREATE TABLE IF NOT EXISTS giveaways (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		message_id TEXT NOT NULL UNIQUE,
		prize TEXT NOT NULL,
		winners_count INTEGER NOT NULL DEFAULT 1,
		created_by TEXT NOT NULL,
		start_time TIMESTAMP NOT NULL,
		end_time TIMESTAMP NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		winners_json TEXT DEFAULT '[]',
		req_role_id TEXT DEFAULT '',
		blacklisted_role_id TEXT DEFAULT '',
		min_account_age_sec INTEGER DEFAULT 0,
		min_server_stay_sec INTEGER DEFAULT 0,
		min_level INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS giveaway_participants (
		giveaway_id INTEGER NOT NULL,
		user_id TEXT NOT NULL,
		joined_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(giveaway_id, user_id),
		FOREIGN KEY(giveaway_id) REFERENCES giveaways(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS afk_users (
		guild_id TEXT NOT NULL DEFAULT '',
		user_id TEXT NOT NULL,
		reason TEXT DEFAULT 'AFK',
		since_timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id)
	);
	CREATE TABLE IF NOT EXISTS embed_templates (
		guild_id TEXT NOT NULL,
		template_name TEXT NOT NULL,
		json_payload TEXT NOT NULL,
		created_by TEXT,
		PRIMARY KEY(guild_id, template_name)
	);
	CREATE TABLE IF NOT EXISTS tags (
		guild_id TEXT NOT NULL,
		tag_name TEXT NOT NULL,
		content TEXT NOT NULL,
		author_id TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, tag_name)
	);
	CREATE TABLE IF NOT EXISTS user_xp (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		xp INTEGER DEFAULT 0,
		level INTEGER DEFAULT 1,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id)
	);
	CREATE TABLE IF NOT EXISTS level_roles (
		guild_id TEXT NOT NULL,
		level INTEGER NOT NULL,
		role_id TEXT NOT NULL,
		PRIMARY KEY(guild_id, level)
	);
	CREATE TABLE IF NOT EXISTS saved_user_roles (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		roles_json TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id)
	);
	CREATE TABLE IF NOT EXISTS reaction_roles (
		message_id TEXT NOT NULL,
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		emoji TEXT NOT NULL,
		role_id TEXT NOT NULL,
		PRIMARY KEY(message_id, emoji)
	);
	CREATE TABLE IF NOT EXISTS user_grails (
		user_id TEXT NOT NULL,
		song_name TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(user_id, song_name)
	);
	CREATE TABLE IF NOT EXISTS lastfm_users (
		user_id TEXT PRIMARY KEY,
		lastfm_username TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS starboard (
		guild_id TEXT NOT NULL,
		original_message_id TEXT NOT NULL,
		starboard_message_id TEXT NOT NULL,
		stars INTEGER DEFAULT 1,
		PRIMARY KEY(guild_id, original_message_id)
	);
	CREATE TABLE IF NOT EXISTS blacklisted_words (
		guild_id TEXT NOT NULL,
		word TEXT NOT NULL,
		PRIMARY KEY(guild_id, word)
	);
	CREATE TABLE IF NOT EXISTS blacklisted_regexes (
		guild_id TEXT NOT NULL,
		pattern TEXT NOT NULL,
		PRIMARY KEY(guild_id, pattern)
	);
	CREATE TABLE IF NOT EXISTS allowed_invites (
		guild_id TEXT NOT NULL,
		invite TEXT NOT NULL,
		PRIMARY KEY(guild_id, invite)
	);
	CREATE TABLE IF NOT EXISTS crowns (
		guild_id TEXT NOT NULL,
		artist_name TEXT NOT NULL,
		user_id TEXT NOT NULL,
		playcount INTEGER NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, artist_name)
	);
	CREATE TABLE IF NOT EXISTS guild_locked_nicknames (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		nickname TEXT NOT NULL,
		locked_by TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id)
	);
	CREATE TABLE IF NOT EXISTS guild_temp_roles (
		guild_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		role_id TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		assigned_by TEXT DEFAULT '',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(guild_id, user_id, role_id)
	);
	CREATE TABLE IF NOT EXISTS guild_temp_voice_channels (
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL PRIMARY KEY,
		owner_id TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
`
