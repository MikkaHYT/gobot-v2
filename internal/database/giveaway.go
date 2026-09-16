package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type GiveawayRecord struct {
	ID                int64     `json:"id"`
	GuildID           string    `json:"guild_id"`
	ChannelID         string    `json:"channel_id"`
	MessageID         string    `json:"message_id"`
	Prize             string    `json:"prize"`
	WinnersCount      int       `json:"winners_count"`
	CreatedBy         string    `json:"created_by"`
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	Status            string    `json:"status"`
	Winners           []string  `json:"winners"`
	ReqRoleID         string    `json:"req_role_id"`
	BlacklistedRoleID string    `json:"blacklisted_role_id"`
	MinAccountAgeSec  int64     `json:"min_account_age_sec"`
	MinServerStaySec  int64     `json:"min_server_stay_sec"`
	MinLevel          int       `json:"min_level"`
}

const giveawaySelectFields = `
	id, guild_id, channel_id, message_id, prize, winners_count, created_by,
	start_time, end_time, status, winners_json, req_role_id, blacklisted_role_id,
	min_account_age_sec, min_server_stay_sec, min_level`

type scannable interface {
	Scan(dest ...any) error
}

func scanGiveaway(scanner scannable) (*GiveawayRecord, error) {
	var rec GiveawayRecord
	var winnersJSON string
	var startStr, endStr string

	err := scanner.Scan(
		&rec.ID,
		&rec.GuildID,
		&rec.ChannelID,
		&rec.MessageID,
		&rec.Prize,
		&rec.WinnersCount,
		&rec.CreatedBy,
		&startStr,
		&endStr,
		&rec.Status,
		&winnersJSON,
		&rec.ReqRoleID,
		&rec.BlacklistedRoleID,
		&rec.MinAccountAgeSec,
		&rec.MinServerStaySec,
		&rec.MinLevel,
	)
	if err != nil {
		return nil, err
	}

	rec.StartTime = parseTimestamp(startStr)
	rec.EndTime = parseTimestamp(endStr)

	rec.Winners = make([]string, 0)
	if winnersJSON != "" && winnersJSON != "[]" {
		if err := json.Unmarshal([]byte(winnersJSON), &rec.Winners); err != nil {
			return nil, fmt.Errorf("failed to unmarshal winners JSON: %w", err)
		}
	}

	return &rec, nil
}

type CreateGiveawayParams struct {
	GuildID           string
	ChannelID         string
	MessageID         string
	Prize             string
	WinnersCount      int
	CreatedBy         string
	StartTime         time.Time
	EndTime           time.Time
	ReqRoleID         string
	BlacklistedRoleID string
	MinAccountAgeSec  int64
	MinServerStaySec  int64
	MinLevel          int
}

func (d *DB) CreateGiveaway(params CreateGiveawayParams) (*GiveawayRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO giveaways (
		guild_id, channel_id, message_id, prize, winners_count, created_by,
		start_time, end_time, status, winners_json, req_role_id, blacklisted_role_id,
		min_account_age_sec, min_server_stay_sec, min_level
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', '[]', ?, ?, ?, ?, ?);`

	res, err := d.conn.ExecContext(
		ctx, query,
		params.GuildID, params.ChannelID, params.MessageID, params.Prize, params.WinnersCount, params.CreatedBy,
		params.StartTime.UTC().Format(time.RFC3339), params.EndTime.UTC().Format(time.RFC3339),
		params.ReqRoleID, params.BlacklistedRoleID, params.MinAccountAgeSec, params.MinServerStaySec, params.MinLevel,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert giveaway: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve giveaway last insert id: %w", err)
	}

	return &GiveawayRecord{
		ID:                id,
		GuildID:           params.GuildID,
		ChannelID:         params.ChannelID,
		MessageID:         params.MessageID,
		Prize:             params.Prize,
		WinnersCount:      params.WinnersCount,
		CreatedBy:         params.CreatedBy,
		StartTime:         params.StartTime,
		EndTime:           params.EndTime,
		Status:            "active",
		Winners:           make([]string, 0),
		ReqRoleID:         params.ReqRoleID,
		BlacklistedRoleID: params.BlacklistedRoleID,
		MinAccountAgeSec:  params.MinAccountAgeSec,
		MinServerStaySec:  params.MinServerStaySec,
		MinLevel:          params.MinLevel,
	}, nil
}

func (d *DB) GetGiveawayByMessageID(messageID string) (*GiveawayRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM giveaways WHERE message_id = ?;`, giveawaySelectFields)
	row := d.conn.QueryRowContext(ctx, query, messageID)
	rec, err := scanGiveaway(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get giveaway by message id %s: %w", messageID, err)
	}

	return rec, nil
}

func (d *DB) GetGiveawayByID(id int64) (*GiveawayRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM giveaways WHERE id = ?;`, giveawaySelectFields)
	row := d.conn.QueryRowContext(ctx, query, id)
	rec, err := scanGiveaway(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get giveaway by id %d: %w", id, err)
	}

	return rec, nil
}

func (d *DB) AddGiveawayParticipant(giveawayID int64, userID string) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT OR IGNORE INTO giveaway_participants (giveaway_id, user_id, joined_at)
	VALUES (?, ?, ?);`

	res, err := d.conn.ExecContext(ctx, query, giveawayID, userID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("failed to add giveaway participant: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to check rows affected: %w", err)
	}

	return rows > 0, nil
}

func (d *DB) RemoveGiveawayParticipant(giveawayID int64, userID string) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM giveaway_participants WHERE giveaway_id = ? AND user_id = ?;`
	res, err := d.conn.ExecContext(ctx, query, giveawayID, userID)
	if err != nil {
		return false, fmt.Errorf("failed to remove giveaway participant: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to check rows affected: %w", err)
	}

	return rows > 0, nil
}

func (d *DB) GetGiveawayParticipants(giveawayID int64) ([]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT user_id FROM giveaway_participants WHERE giveaway_id = ? ORDER BY joined_at ASC;`
	rows, err := d.conn.QueryContext(ctx, query, giveawayID)
	if err != nil {
		return nil, fmt.Errorf("failed to query giveaway participants: %w", err)
	}
	defer rows.Close()

	users := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("failed to scan giveaway participant: %w", err)
		}
		users = append(users, uid)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating giveaway participants: %w", err)
	}

	return users, nil
}

func (d *DB) GetGiveawayParticipantCount(giveawayID int64) (int, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT COUNT(*) FROM giveaway_participants WHERE giveaway_id = ?;`
	var count int
	err := d.conn.QueryRowContext(ctx, query, giveawayID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count giveaway participants: %w", err)
	}
	return count, nil
}

func (d *DB) UpdateGiveawayStatusAndWinners(giveawayID int64, status string, winners []string, endTime time.Time) error {
	ctx, cancel := newCtx()
	defer cancel()

	if winners == nil {
		winners = make([]string, 0)
	}
	winnersJSON, err := json.Marshal(winners)
	if err != nil {
		return fmt.Errorf("failed to marshal winners: %w", err)
	}

	query := `UPDATE giveaways SET status = ?, winners_json = ?, end_time = ? WHERE id = ?;`
	_, err = d.conn.ExecContext(ctx, query, status, string(winnersJSON), endTime.UTC().Format(time.RFC3339), giveawayID)
	if err != nil {
		return fmt.Errorf("failed to update giveaway status: %w", err)
	}
	return nil
}

func (d *DB) EndGiveawayAtomic(giveawayID int64, winners []string, endTime time.Time) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	if winners == nil {
		winners = make([]string, 0)
	}
	winnersJSON, err := json.Marshal(winners)
	if err != nil {
		return false, fmt.Errorf("failed to marshal winners: %w", err)
	}

	query := `UPDATE giveaways SET status = 'ended', winners_json = ?, end_time = ? WHERE id = ? AND status = 'active';`
	res, err := d.conn.ExecContext(ctx, query, string(winnersJSON), endTime.UTC().Format(time.RFC3339), giveawayID)
	if err != nil {
		return false, fmt.Errorf("failed to atomically end giveaway: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (d *DB) GetGuildGiveaways(guildID string) ([]*GiveawayRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM giveaways WHERE guild_id = ? ORDER BY start_time DESC;`, giveawaySelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query guild giveaways: %w", err)
	}
	defer rows.Close()

	results := make([]*GiveawayRecord, 0)
	for rows.Next() {
		rec, err := scanGiveaway(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan giveaway: %w", err)
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating guild giveaways: %w", err)
	}

	return results, nil
}

func (d *DB) GetActiveGiveaways() ([]*GiveawayRecord, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM giveaways WHERE status = 'active';`, giveawaySelectFields)
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active giveaways: %w", err)
	}
	defer rows.Close()

	results := make([]*GiveawayRecord, 0)
	for rows.Next() {
		rec, err := scanGiveaway(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan active giveaway: %w", err)
		}
		results = append(results, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active giveaways: %w", err)
	}

	return results, nil
}

func (d *DB) CancelGiveawayAtomic(giveawayID int64) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `UPDATE giveaways SET status = 'cancelled', end_time = ? WHERE id = ? AND status = 'active';`
	res, err := d.conn.ExecContext(ctx, query, time.Now().UTC().Format(time.RFC3339), giveawayID)
	if err != nil {
		return false, fmt.Errorf("failed to cancel giveaway: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to verify giveaway cancellation: %w", err)
	}
	return n > 0, nil
}
