package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AFKUser struct {
	GuildID        string    `json:"guild_id"`
	UserID         string    `json:"user_id"`
	Reason         string    `json:"reason"`
	SinceTimestamp time.Time `json:"since_timestamp"`
}

func (d *DB) SetUserAFK(guildID, userID, reason string) error {
	ctx, cancel := newCtx()
	defer cancel()

	if reason == "" {
		reason = "AFK"
	}

	query := `
	INSERT INTO afk_users (guild_id, user_id, reason, since_timestamp) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET reason = excluded.reason, since_timestamp = CURRENT_TIMESTAMP;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, reason); err != nil {
		return fmt.Errorf("failed to set AFK for user %s in guild %s: %w", userID, guildID, err)
	}
	return nil
}

func (d *DB) RemoveUserAFK(guildID, userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM afk_users WHERE guild_id = ? AND user_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID); err != nil {
		return fmt.Errorf("failed to remove AFK for user %s in guild %s: %w", userID, guildID, err)
	}
	return nil
}
func (d *DB) GetUserAFKInfo(guildID, userID string) (*AFKUser, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, reason, since_timestamp FROM afk_users WHERE guild_id = ? AND user_id = ? LIMIT 1`
	var a AFKUser
	var ts string
	err := d.conn.QueryRowContext(ctx, query, guildID, userID).Scan(&a.GuildID, &a.UserID, &a.Reason, &ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get AFK info for user %s in guild %s: %w", userID, guildID, err)
	}
	a.SinceTimestamp = parseTimestamp(ts)
	return &a, nil
}
