package database

import (
	"context"
	"fmt"
	"strings"
)

func (d *DB) queryStringSlice(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (d *DB) GetBlacklistedWords(guildID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT word FROM blacklisted_words WHERE guild_id = ? ORDER BY word ASC`
	return d.queryStringSlice(ctx, query, guildID)
}

func (d *DB) AddBlacklistedWord(guildID, word string) error {
	ctx, cancel := newCtx()
	defer cancel()

	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}

	query := `INSERT OR IGNORE INTO blacklisted_words (guild_id, word) VALUES (?, ?)`
	if _, err := d.conn.ExecContext(ctx, query, guildID, word); err != nil {
		return fmt.Errorf("failed to add blacklisted word: %w", err)
	}
	return nil
}

func (d *DB) RemoveBlacklistedWord(guildID, word string) error {
	ctx, cancel := newCtx()
	defer cancel()

	word = strings.ToLower(strings.TrimSpace(word))
	query := `DELETE FROM blacklisted_words WHERE guild_id = ? AND word = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, word); err != nil {
		return fmt.Errorf("failed to remove blacklisted word: %w", err)
	}
	return nil
}

func (d *DB) ClearBlacklistedWords(guildID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM blacklisted_words WHERE guild_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID); err != nil {
		return fmt.Errorf("failed to clear blacklisted words: %w", err)
	}
	return nil
}

func (d *DB) GetBlacklistedRegexes(guildID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT pattern FROM blacklisted_regexes WHERE guild_id = ? ORDER BY pattern ASC`
	return d.queryStringSlice(ctx, query, guildID)
}

func (d *DB) AddBlacklistedRegex(guildID, pattern string) error {
	ctx, cancel := newCtx()
	defer cancel()

	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}

	query := `INSERT OR IGNORE INTO blacklisted_regexes (guild_id, pattern) VALUES (?, ?)`
	if _, err := d.conn.ExecContext(ctx, query, guildID, pattern); err != nil {
		return fmt.Errorf("failed to add blacklisted regex: %w", err)
	}
	return nil
}

func (d *DB) RemoveBlacklistedRegex(guildID, pattern string) error {
	ctx, cancel := newCtx()
	defer cancel()

	pattern = strings.TrimSpace(pattern)
	query := `DELETE FROM blacklisted_regexes WHERE guild_id = ? AND pattern = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, pattern); err != nil {
		return fmt.Errorf("failed to remove blacklisted regex: %w", err)
	}
	return nil
}

func (d *DB) ClearBlacklistedRegexes(guildID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM blacklisted_regexes WHERE guild_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID); err != nil {
		return fmt.Errorf("failed to clear blacklisted regexes: %w", err)
	}
	return nil
}

func (d *DB) AddBlacklistedWords(guildID string, words []string) (int, error) {
	ctx, cancel := newCtx()
	defer cancel()

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO blacklisted_words (guild_id, word) VALUES (?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	added := 0
	for _, raw := range words {
		w := strings.ToLower(strings.TrimSpace(raw))
		if w == "" {
			continue
		}
		res, execErr := stmt.ExecContext(ctx, guildID, w)
		if execErr != nil {
			return added, execErr
		}
		if rows, _ := res.RowsAffected(); rows > 0 {
			added++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

func (d *DB) GetAllowedInvites(guildID string) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT invite FROM allowed_invites WHERE guild_id = ? ORDER BY invite ASC`
	return d.queryStringSlice(ctx, query, guildID)
}

func (d *DB) AddAllowedInvite(guildID, invite string) error {
	ctx, cancel := newCtx()
	defer cancel()

	invite = strings.ToLower(strings.TrimSpace(invite))
	if invite == "" {
		return nil
	}

	query := `INSERT OR IGNORE INTO allowed_invites (guild_id, invite) VALUES (?, ?)`
	if _, err := d.conn.ExecContext(ctx, query, guildID, invite); err != nil {
		return fmt.Errorf("failed to add allowed invite: %w", err)
	}
	return nil
}

func (d *DB) RemoveAllowedInvite(guildID, invite string) error {
	ctx, cancel := newCtx()
	defer cancel()

	invite = strings.ToLower(strings.TrimSpace(invite))
	query := `DELETE FROM allowed_invites WHERE guild_id = ? AND invite = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, invite); err != nil {
		return fmt.Errorf("failed to remove allowed invite: %w", err)
	}
	return nil
}

func (d *DB) ClearAllowedInvites(guildID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM allowed_invites WHERE guild_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID); err != nil {
		return fmt.Errorf("failed to clear allowed invites: %w", err)
	}
	return nil
}

