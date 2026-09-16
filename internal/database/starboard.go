package database

import (
	"database/sql"
	"errors"
	"fmt"
)

type StarboardEntry struct {
	GuildID            string `json:"guild_id"`
	OriginalMessageID  string `json:"original_message_id"`
	StarboardMessageID string `json:"starboard_message_id"`
	Stars              int    `json:"stars"`
}

const starboardSelectFields = `guild_id, original_message_id, starboard_message_id, stars`

func scanStarboardEntry(scanner scannable) (*StarboardEntry, error) {
	var entry StarboardEntry
	err := scanner.Scan(&entry.GuildID, &entry.OriginalMessageID, &entry.StarboardMessageID, &entry.Stars)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (d *DB) GetStarboardEntry(guildID, origMsgID string) (*StarboardEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM starboard WHERE guild_id = ? AND original_message_id = ?`, starboardSelectFields)
	row := d.conn.QueryRowContext(ctx, query, guildID, origMsgID)

	entry, err := scanStarboardEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get starboard entry: %w", err)
	}
	return entry, nil
}

func (d *DB) SaveStarboardEntry(guildID, origMsgID, starMsgID string, stars int) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO starboard (guild_id, original_message_id, starboard_message_id, stars) VALUES (?, ?, ?, ?)
	ON CONFLICT(guild_id, original_message_id) DO UPDATE SET starboard_message_id = excluded.starboard_message_id, stars = excluded.stars;`

	if _, err := d.conn.ExecContext(ctx, query, guildID, origMsgID, starMsgID, stars); err != nil {
		return fmt.Errorf("failed to save starboard entry: %w", err)
	}
	return nil
}

func (d *DB) DeleteStarboardEntry(guildID, origMsgID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM starboard WHERE guild_id = ? AND original_message_id = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, origMsgID)
	if err != nil {
		return fmt.Errorf("failed to delete starboard entry: %w", err)
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
