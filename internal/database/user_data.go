package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	tagSelectFields          = `guild_id, tag_name, content, author_id, created_at`
	reactionRoleSelectFields = `message_id, guild_id, channel_id, emoji, role_id`
	crownSelectFields        = `guild_id, artist_name, user_id, playcount, updated_at`
)

type TagInfo struct {
	GuildID   string    `json:"guild_id"`
	TagName   string    `json:"tag_name"`
	Content   string    `json:"content"`
	AuthorID  string    `json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ReactionRoleBinding struct {
	MessageID string `json:"message_id"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Emoji     string `json:"emoji"`
	RoleID    string `json:"role_id"`
}

type CrownInfo struct {
	GuildID    string    `json:"guild_id"`
	ArtistName string    `json:"artist_name"`
	UserID     string    `json:"user_id"`
	Playcount  int       `json:"playcount"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TempVoiceChannelRecord struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	OwnerID   string `json:"owner_id"`
}

func scanTagInfo(scanner scannable) (*TagInfo, error) {
	var t TagInfo
	var createdStr string
	err := scanner.Scan(&t.GuildID, &t.TagName, &t.Content, &t.AuthorID, &createdStr)
	if err != nil {
		return nil, err
	}
	t.CreatedAt = parseTimestamp(createdStr)
	return &t, nil
}

func scanReactionRoleBinding(scanner scannable) (*ReactionRoleBinding, error) {
	var b ReactionRoleBinding
	err := scanner.Scan(&b.MessageID, &b.GuildID, &b.ChannelID, &b.Emoji, &b.RoleID)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func scanCrownInfo(scanner scannable) (*CrownInfo, error) {
	var c CrownInfo
	var updatedStr string
	err := scanner.Scan(&c.GuildID, &c.ArtistName, &c.UserID, &c.Playcount, &updatedStr)
	if err != nil {
		return nil, err
	}
	c.UpdatedAt = parseTimestamp(updatedStr)
	return &c, nil
}

func (d *DB) GetTagInfoByGuildID(guildID, tagName string) (*TagInfo, error) {
	ctx, cancel := newCtx()
	defer cancel()

	tagName = strings.ToLower(strings.TrimSpace(tagName))
	query := fmt.Sprintf(`SELECT %s FROM tags WHERE guild_id = ? AND tag_name = ?`, tagSelectFields)
	row := d.conn.QueryRowContext(ctx, query, guildID, tagName)

	t, err := scanTagInfo(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get tag %q: %w", tagName, err)
	}
	return t, nil
}

func (d *DB) GetTagContentByGuildID(guildID, tagName string) (string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	tagName = strings.ToLower(strings.TrimSpace(tagName))
	query := `SELECT content FROM tags WHERE guild_id = ? AND tag_name = ?`
	var content string

	err := d.conn.QueryRowContext(ctx, query, guildID, tagName).Scan(&content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get tag content %q: %w", tagName, err)
	}
	return content, nil
}

func (d *DB) SaveGuildTag(guildID, tagName, content, authorID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	tagName = strings.ToLower(strings.TrimSpace(tagName))
	query := `
	INSERT INTO tags (guild_id, tag_name, content, author_id) VALUES (?, ?, ?, ?)
	ON CONFLICT(guild_id, tag_name) DO UPDATE SET content = excluded.content, author_id = excluded.author_id;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, tagName, content, authorID); err != nil {
		return fmt.Errorf("failed to save tag %q: %w", tagName, err)
	}
	return nil
}

func (d *DB) DeleteGuildTag(guildID, tagName string) error {
	ctx, cancel := newCtx()
	defer cancel()

	tagName = strings.ToLower(strings.TrimSpace(tagName))
	query := `DELETE FROM tags WHERE guild_id = ? AND tag_name = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, tagName)
	if err != nil {
		return fmt.Errorf("failed to delete tag %q: %w", tagName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListGuildTags(guildID string) ([]TagInfo, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, tag_name, content, author_id, created_at FROM tags WHERE guild_id = ? ORDER BY tag_name`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	tags := make([]TagInfo, 0)
	for rows.Next() {
		t, err := scanTagInfo(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tag info: %w", err)
		}
		tags = append(tags, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tag list: %w", err)
	}
	return tags, nil
}

func escapeLikePattern(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '%' || r == '_' || r == '\\' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (d *DB) SearchGuildTags(guildID, queryStr string) ([]TagInfo, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, tag_name, content, author_id, created_at FROM tags WHERE guild_id = ? AND LOWER(tag_name) LIKE LOWER(?) ESCAPE '\' ORDER BY tag_name`
	escapedQuery := "%" + escapeLikePattern(strings.TrimSpace(queryStr)) + "%"
	rows, err := d.conn.QueryContext(ctx, query, guildID, escapedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to search tags for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	tags := make([]TagInfo, 0)
	for rows.Next() {
		t, err := scanTagInfo(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tag info: %w", err)
		}
		tags = append(tags, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tag search: %w", err)
	}
	return tags, nil
}

func (d *DB) SaveEmbedTemplate(guildID, templateName, jsonPayload, createdBy string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO embed_templates (guild_id, template_name, json_payload, created_by) VALUES (?, ?, ?, ?)
	ON CONFLICT(guild_id, template_name) DO UPDATE SET json_payload = excluded.json_payload, created_by = excluded.created_by;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, templateName, jsonPayload, createdBy); err != nil {
		return fmt.Errorf("failed to save embed template %q: %w", templateName, err)
	}
	return nil
}

func (d *DB) GetEmbedTemplate(guildID, templateName string) (string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT json_payload FROM embed_templates WHERE guild_id = ? AND template_name = ?`
	var payload string
	err := d.conn.QueryRowContext(ctx, query, guildID, templateName).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get embed template %q: %w", templateName, err)
	}
	return payload, nil
}

func (d *DB) DeleteEmbedTemplate(guildID, templateName string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM embed_templates WHERE guild_id = ? AND template_name = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, templateName)
	if err != nil {
		return fmt.Errorf("failed to delete embed template %q: %w", templateName, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListEmbedTemplates(guildID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT template_name FROM embed_templates WHERE guild_id = ? ORDER BY template_name`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to list embed templates: %w", err)
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan embed template name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating embed templates: %w", err)
	}
	return names, nil
}

func (d *DB) SaveReactionRoleBinding(msgID, guildID, channelID, emoji, roleID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO reaction_roles (message_id, guild_id, channel_id, emoji, role_id) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(message_id, emoji) DO UPDATE SET role_id = excluded.role_id, channel_id = excluded.channel_id;`
	if _, err := d.conn.ExecContext(ctx, query, msgID, guildID, channelID, emoji, roleID); err != nil {
		return fmt.Errorf("failed to save reaction role binding: %w", err)
	}
	return nil
}

func (d *DB) DeleteReactionRoleBinding(guildID, msgID, emoji string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM reaction_roles WHERE guild_id = ? AND message_id = ? AND emoji = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, msgID, emoji)
	if err != nil {
		return fmt.Errorf("failed to delete reaction role binding: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteAllReactionRolesForMessage(guildID, msgID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM reaction_roles WHERE guild_id = ? AND message_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, msgID); err != nil {
		return fmt.Errorf("failed to delete reaction roles for message %s: %w", msgID, err)
	}
	return nil
}

func (d *DB) GetReactionRolesForMessage(guildID, msgID string) ([]ReactionRoleBinding, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM reaction_roles WHERE guild_id = ? AND message_id = ?`, reactionRoleSelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID, msgID)
	if err != nil {
		return nil, fmt.Errorf("failed to query reaction roles for message %s: %w", msgID, err)
	}
	defer rows.Close()

	bindings := make([]ReactionRoleBinding, 0)
	for rows.Next() {
		b, err := scanReactionRoleBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reaction role binding: %w", err)
		}
		bindings = append(bindings, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reaction roles: %w", err)
	}
	return bindings, nil
}

func (d *DB) GetReactionRoleID(guildID, msgID, emoji string) (string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	var query string
	var row *sql.Row
	if guildID != "" {
		query = `SELECT role_id FROM reaction_roles WHERE guild_id = ? AND message_id = ? AND emoji = ?`
		row = d.conn.QueryRowContext(ctx, query, guildID, msgID, emoji)
	} else {
		query = `SELECT role_id FROM reaction_roles WHERE message_id = ? AND emoji = ?`
		row = d.conn.QueryRowContext(ctx, query, msgID, emoji)
	}

	var roleID string
	err := row.Scan(&roleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get reaction role: %w", err)
	}
	return roleID, nil
}

func (d *DB) GetGuildReactionRoles(guildID string) ([]ReactionRoleBinding, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM reaction_roles WHERE guild_id = ?`, reactionRoleSelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query reaction roles for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	bindings := make([]ReactionRoleBinding, 0)
	for rows.Next() {
		b, err := scanReactionRoleBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reaction role binding: %w", err)
		}
		bindings = append(bindings, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reaction roles: %w", err)
	}
	return bindings, nil
}

func (d *DB) AddUserGrailSongs(userID string, songs ...string) error {
	if len(songs) == 0 {
		return nil
	}
	ctx, cancel := newCtx()
	defer cancel()

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO user_grails (user_id, song_name) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	for _, song := range songs {
		if song = strings.TrimSpace(song); song != "" {
			if _, err := stmt.ExecContext(ctx, userID, song); err != nil {
				return fmt.Errorf("failed to insert grail song %q for user %s: %w", song, userID, err)
			}
		}
	}
	return tx.Commit()
}

func (d *DB) DeleteUserGrailSongs(userID string, songs ...string) error {
	if len(songs) == 0 {
		return nil
	}
	ctx, cancel := newCtx()
	defer cancel()

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, "DELETE FROM user_grails WHERE user_id = ? AND song_name = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer stmt.Close()

	for _, song := range songs {
		if song = strings.TrimSpace(song); song != "" {
			if _, err := stmt.ExecContext(ctx, userID, song); err != nil {
				return fmt.Errorf("failed to delete grail song %q for user %s: %w", song, userID, err)
			}
		}
	}
	return tx.Commit()
}

func (d *DB) GetUserGrailSongs(userID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT song_name FROM user_grails WHERE user_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query grail songs for user %s: %w", userID, err)
	}
	defer rows.Close()

	songs := make([]string, 0)
	for rows.Next() {
		var song string
		if err := rows.Scan(&song); err != nil {
			return nil, fmt.Errorf("failed to scan grail song: %w", err)
		}
		songs = append(songs, song)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating grail songs: %w", err)
	}
	return songs, nil
}

func (d *DB) ClearAllUserGrails(userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM user_grails WHERE user_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("failed to clear grails for user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) SetLastfmUsername(userID, username string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO lastfm_users (user_id, lastfm_username) VALUES (?, ?)
	ON CONFLICT(user_id) DO UPDATE SET lastfm_username = excluded.lastfm_username;`
	if _, err := d.conn.ExecContext(ctx, query, userID, username); err != nil {
		return fmt.Errorf("failed to set lastfm username for user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) GetLastfmUsername(userID string) (string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT lastfm_username FROM lastfm_users WHERE user_id = ?`
	var lastfmUsername string

	err := d.conn.QueryRowContext(ctx, query, userID).Scan(&lastfmUsername)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get lastfm username for user %s: %w", userID, err)
	}
	return lastfmUsername, nil
}

func (d *DB) DeleteLastfmUsername(userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM lastfm_users WHERE user_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("failed to delete lastfm username for user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) GetAllLastfmUsersMap() (map[string]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT user_id, lastfm_username FROM lastfm_users`
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all lastfm users: %w", err)
	}
	defer rows.Close()

	users := make(map[string]string)
	for rows.Next() {
		var uid, uname string
		if err := rows.Scan(&uid, &uname); err != nil {
			return nil, fmt.Errorf("failed to scan lastfm user: %w", err)
		}
		users[uid] = uname
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lastfm users: %w", err)
	}
	return users, nil
}

func (d *DB) GetCrown(guildID, artistName string) (*CrownInfo, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM crowns WHERE guild_id = ? AND LOWER(artist_name) = LOWER(?)`, crownSelectFields)
	row := d.conn.QueryRowContext(ctx, query, guildID, artistName)

	c, err := scanCrownInfo(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get crown for %s: %w", artistName, err)
	}
	return c, nil
}

func (d *DB) SetCrown(guildID, artistName, userID string, playcount int) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO crowns (guild_id, artist_name, user_id, playcount, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, artist_name) DO UPDATE SET user_id = excluded.user_id, playcount = excluded.playcount, updated_at = CURRENT_TIMESTAMP;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, artistName, userID, playcount); err != nil {
		return fmt.Errorf("failed to set crown for artist %s: %w", artistName, err)
	}
	return nil
}

func (d *DB) GetUserCrowns(guildID, userID string) ([]CrownInfo, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM crowns WHERE guild_id = ? AND user_id = ? ORDER BY playcount DESC`, crownSelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query crowns for user %s: %w", userID, err)
	}
	defer rows.Close()

	crowns := make([]CrownInfo, 0)
	for rows.Next() {
		c, err := scanCrownInfo(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan crown: %w", err)
		}
		crowns = append(crowns, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user crowns: %w", err)
	}
	return crowns, nil
}

func (d *DB) SaveUserRoles(guildID, userID string, roles []string) error {
	ctx, cancel := newCtx()
	defer cancel()

	if roles == nil {
		roles = make([]string, 0)
	}
	rolesBytes, err := marshalStringSlice(roles, "saved_user_roles")
	if err != nil {
		return err
	}
	query := `
	INSERT INTO saved_user_roles (guild_id, user_id, roles_json, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET roles_json = excluded.roles_json, updated_at = CURRENT_TIMESTAMP;`
	_, err = d.conn.ExecContext(ctx, query, guildID, userID, string(rolesBytes))
	if err != nil {
		return fmt.Errorf("failed to save user roles: %w", err)
	}
	return nil
}

func (d *DB) GetSavedUserRoles(guildID, userID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT roles_json FROM saved_user_roles WHERE guild_id = ? AND user_id = ?`
	var rolesJSON string
	err := d.conn.QueryRowContext(ctx, query, guildID, userID).Scan(&rolesJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get saved user roles: %w", err)
	}

	roles := make([]string, 0)
	if err := unmarshalStringSlice(rolesJSON, "saved_user_roles", userID, &roles); err != nil {
		return nil, err
	}
	return roles, nil
}

func (d *DB) PruneOldSavedUserRoles(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM saved_user_roles WHERE updated_at < datetime('now', '-' || ? || ' days')`
	res, err := d.conn.ExecContext(ctx, query, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("failed to prune saved user roles: %w", err)
	}
	return res.RowsAffected()
}

func (d *DB) SaveTempVoiceChannel(guildID, channelID, ownerID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO guild_temp_voice_channels (guild_id, channel_id, owner_id) VALUES (?, ?, ?)
	ON CONFLICT(channel_id) DO UPDATE SET owner_id = excluded.owner_id;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, channelID, ownerID); err != nil {
		return fmt.Errorf("failed to save temp voice channel: %w", err)
	}
	return nil
}

func (d *DB) DeleteTempVoiceChannel(channelID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM guild_temp_voice_channels WHERE channel_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, channelID); err != nil {
		return fmt.Errorf("failed to delete temp voice channel: %w", err)
	}
	return nil
}

func (d *DB) GetAllTempVoiceChannels() ([]TempVoiceChannelRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, channel_id, owner_id FROM guild_temp_voice_channels`
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query temp voice channels: %w", err)
	}
	defer rows.Close()

	list := make([]TempVoiceChannelRecord, 0)
	for rows.Next() {
		var rec TempVoiceChannelRecord
		if err := rows.Scan(&rec.GuildID, &rec.ChannelID, &rec.OwnerID); err != nil {
			return nil, fmt.Errorf("failed to scan temp voice channel: %w", err)
		}
		list = append(list, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating temp voice channels: %w", err)
	}
	return list, nil
}

func (d *DB) UpdateTempVoiceChannelOwner(channelID, newOwnerID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `UPDATE guild_temp_voice_channels SET owner_id = ? WHERE channel_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, newOwnerID, channelID); err != nil {
		return fmt.Errorf("failed to update temp voice channel owner: %w", err)
	}
	return nil
}
