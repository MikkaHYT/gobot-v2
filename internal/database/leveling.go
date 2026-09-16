package database

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

type UserLevelData struct {
	GuildID   string    `json:"guild_id"`
	UserID    string    `json:"user_id"`
	XP        int64     `json:"xp"`
	Level     int       `json:"level"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LevelRoleReward struct {
	GuildID string `json:"guild_id"`
	Level   int    `json:"level"`
	RoleID  string `json:"role_id"`
}

const MaxLevel = 1000

const MaxSafeXP = int64(100 * (MaxLevel - 1) * (MaxLevel - 1))

func CalculateLevel(xp int64) int {
	if xp <= 0 {
		return 1
	}
	level := int(math.Sqrt(float64(xp)/100.0)) + 1
	if level < 1 {
		return 1
	}
	if level > MaxLevel {
		return MaxLevel
	}
	return level
}

func RequiredXPForLevel(level int) int64 {
	if level <= 1 {
		return 0
	}
	if level > MaxLevel {
		level = MaxLevel
	}
	return int64(100 * (level - 1) * (level - 1))
}

func XPProgress(xp int64) (currentLevel int, currentLevelXP int64, nextLevelXP int64, progressPercent float64) {
	if xp < 0 {
		xp = 0
	}
	currentLevel = CalculateLevel(xp)
	baseXP := RequiredXPForLevel(currentLevel)
	targetXP := RequiredXPForLevel(currentLevel + 1)
	rangeXP := targetXP - baseXP

	currentLevelXP = xp - baseXP
	nextLevelXP = rangeXP
	if rangeXP > 0 {
		progressPercent = math.Min(100.0, math.Max(0.0, float64(currentLevelXP)/float64(rangeXP)*100.0))
	}
	return currentLevel, currentLevelXP, nextLevelXP, progressPercent
}

func (d *DB) GetUserXP(guildID, userID string) (*UserLevelData, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, xp, level, updated_at FROM user_xp WHERE guild_id = ? AND user_id = ?;`
	row := d.conn.QueryRowContext(ctx, query, guildID, userID)

	var data UserLevelData
	var ts string
	err := row.Scan(&data.GuildID, &data.UserID, &data.XP, &data.Level, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return &UserLevelData{
			GuildID:   guildID,
			UserID:    userID,
			XP:        0,
			Level:     1,
			UpdatedAt: time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user xp: %w", err)
	}

	data.UpdatedAt = parseTimestamp(ts)
	return &data, nil
}

func (d *DB) AddUserXP(guildID, userID string, amount int64) (int, bool, error) {
	if amount <= 0 {
		current, err := d.GetUserXP(guildID, userID)
		if err != nil {
			return 1, false, err
		}
		return current.Level, false, nil
	}

	ctx, cancel := newCtx()
	defer cancel()

	tx, err := d.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 1, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var oldXP int64
	var oldLevel int
	querySelect := `SELECT xp, level FROM user_xp WHERE guild_id = ? AND user_id = ?;`
	err = tx.QueryRowContext(ctx, querySelect, guildID, userID).Scan(&oldXP, &oldLevel)
	if errors.Is(err, sql.ErrNoRows) {
		oldXP = 0
		oldLevel = 1
	} else if err != nil {
		return 1, false, err
	}

	newXP := oldXP + amount
	if newXP < oldXP || newXP > MaxSafeXP {
		newXP = MaxSafeXP
	}
	newLevel := CalculateLevel(newXP)
	leveledUp := newLevel > oldLevel

	queryUpsert := `
	INSERT INTO user_xp (guild_id, user_id, xp, level, updated_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET
		xp = excluded.xp,
		level = excluded.level,
		updated_at = CURRENT_TIMESTAMP;`

	_, err = tx.ExecContext(ctx, queryUpsert, guildID, userID, newXP, newLevel)
	if err != nil {
		return oldLevel, false, fmt.Errorf("failed to update user xp: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return oldLevel, false, err
	}

	return newLevel, leveledUp, nil
}

func (d *DB) GetTopXPLeaderboard(guildID string, limit int) ([]UserLevelData, error) {
	if limit <= 0 {
		limit = 10
	}

	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, xp, level, updated_at FROM user_xp WHERE guild_id = ? ORDER BY xp DESC LIMIT ?;`
	rows, err := d.conn.QueryContext(ctx, query, guildID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch leaderboard: %w", err)
	}
	defer rows.Close()

	result := make([]UserLevelData, 0, limit)
	for rows.Next() {
		var data UserLevelData
		var ts string
		if err := rows.Scan(&data.GuildID, &data.UserID, &data.XP, &data.Level, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan leaderboard row: %w", err)
		}
		data.UpdatedAt = parseTimestamp(ts)
		result = append(result, data)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leaderboard iteration error: %w", err)
	}

	return result, nil
}

func (d *DB) GetAllGuildUserXP(guildID string) ([]UserLevelData, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, xp, level, updated_at FROM user_xp WHERE guild_id = ?;`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch all guild user xp: %w", err)
	}
	defer rows.Close()

	result := make([]UserLevelData, 0)
	for rows.Next() {
		var data UserLevelData
		var ts string
		if err := rows.Scan(&data.GuildID, &data.UserID, &data.XP, &data.Level, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan user xp row: %w", err)
		}
		data.UpdatedAt = parseTimestamp(ts)
		result = append(result, data)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user xp iteration error: %w", err)
	}

	return result, nil
}

func (d *DB) AddLevelRole(guildID string, level int, roleID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO level_roles (guild_id, level, role_id)
	VALUES (?, ?, ?)
	ON CONFLICT(guild_id, level) DO UPDATE SET role_id = excluded.role_id;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, level, roleID); err != nil {
		return fmt.Errorf("failed to add level role: %w", err)
	}
	return nil
}

func (d *DB) RemoveLevelRole(guildID string, level int) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM level_roles WHERE guild_id = ? AND level = ?;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, level); err != nil {
		return fmt.Errorf("failed to remove level role: %w", err)
	}
	return nil
}

func (d *DB) GetLevelRoles(guildID string) ([]LevelRoleReward, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, level, role_id FROM level_roles WHERE guild_id = ? ORDER BY level ASC;`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query level roles: %w", err)
	}
	defer rows.Close()

	result := make([]LevelRoleReward, 0)
	for rows.Next() {
		var r LevelRoleReward
		if err := rows.Scan(&r.GuildID, &r.Level, &r.RoleID); err != nil {
			return nil, fmt.Errorf("failed to scan level role: %w", err)
		}
		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating level roles: %w", err)
	}

	return result, nil
}

func (d *DB) SetUserXP(guildID, userID string, amount int64) (int, error) {
	if amount < 0 {
		amount = 0
	}
	if amount > MaxSafeXP {
		amount = MaxSafeXP
	}
	newLevel := CalculateLevel(amount)

	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO user_xp (guild_id, user_id, xp, level, updated_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET
		xp = excluded.xp,
		level = excluded.level,
		updated_at = CURRENT_TIMESTAMP;`

	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, amount, newLevel); err != nil {
		return 1, fmt.Errorf("failed to set user xp: %w", err)
	}
	return newLevel, nil
}

func (d *DB) RemoveUserXP(guildID, userID string, amount int64) (int, int64, error) {
	if amount <= 0 {
		current, err := d.GetUserXP(guildID, userID)
		if err != nil {
			return 1, 0, err
		}
		return current.Level, current.XP, nil
	}

	ctx, cancel := newCtx()
	defer cancel()

	tx, err := d.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 1, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var oldXP int64
	var oldLevel int
	querySelect := `SELECT xp, level FROM user_xp WHERE guild_id = ? AND user_id = ?;`
	err = tx.QueryRowContext(ctx, querySelect, guildID, userID).Scan(&oldXP, &oldLevel)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, 0, nil
	} else if err != nil {
		return 1, 0, err
	}

	newXP := oldXP - amount
	if newXP < 0 {
		newXP = 0
	}
	newLevel := CalculateLevel(newXP)

	queryUpdate := `UPDATE user_xp SET xp = ?, level = ?, updated_at = CURRENT_TIMESTAMP WHERE guild_id = ? AND user_id = ?;`
	if _, err := tx.ExecContext(ctx, queryUpdate, newXP, newLevel, guildID, userID); err != nil {
		return oldLevel, oldXP, fmt.Errorf("failed to remove user xp: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return oldLevel, oldXP, err
	}

	return newLevel, newXP, nil
}

func (d *DB) SetUserLevel(guildID, userID string, level int) (int64, error) {
	if level < 1 {
		level = 1
	}
	if level > MaxLevel {
		level = MaxLevel
	}
	requiredXP := RequiredXPForLevel(level)

	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO user_xp (guild_id, user_id, xp, level, updated_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET
		xp = excluded.xp,
		level = excluded.level,
		updated_at = CURRENT_TIMESTAMP;`

	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, requiredXP, level); err != nil {
		return 0, fmt.Errorf("failed to set user level: %w", err)
	}
	return requiredXP, nil
}

func (d *DB) ResetUserXP(guildID, userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM user_xp WHERE guild_id = ? AND user_id = ?;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID); err != nil {
		return fmt.Errorf("failed to reset user xp: %w", err)
	}
	return nil
}

func (d *DB) GetUserServerRank(guildID, userID string) (int, error) {
	ctx, cancel := newCtx()
	defer cancel()

	userXP, err := d.GetUserXP(guildID, userID)
	if err != nil {
		return 1, err
	}

	query := `SELECT COUNT(*) FROM user_xp WHERE guild_id = ? AND xp > ?;`
	var higherCount int
	err = d.conn.QueryRowContext(ctx, query, guildID, userXP.XP).Scan(&higherCount)
	if err != nil {
		return 1, fmt.Errorf("failed to calculate user rank: %w", err)
	}

	return higherCount + 1, nil
}
