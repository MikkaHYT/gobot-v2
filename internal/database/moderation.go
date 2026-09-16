package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ModCase struct {
	CaseID    int64     `json:"case_id"`
	GuildID   string    `json:"guild_id"`
	UserID    string    `json:"user_id"`
	ModID     string    `json:"mod_id"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type JailedUser struct {
	GuildID   string    `json:"guild_id"`
	UserID    string    `json:"user_id"`
	Roles     []string  `json:"roles"`
	JailedBy  string    `json:"jailed_by"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type TempRoleEntry struct {
	GuildID    string    `json:"guild_id"`
	UserID     string    `json:"user_id"`
	RoleID     string    `json:"role_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	AssignedBy string    `json:"assigned_by"`
	CreatedAt  time.Time `json:"created_at"`
}

const (
	modCaseSelectFields    = `case_id, guild_id, user_id, mod_id, action, reason, created_at`
	jailedUserSelectFields = `guild_id, user_id, roles_json, jailed_by, reason, created_at`
)

func scanModCase(scanner scannable) (*ModCase, error) {
	var c ModCase
	var createdStr string
	err := scanner.Scan(&c.CaseID, &c.GuildID, &c.UserID, &c.ModID, &c.Action, &c.Reason, &createdStr)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = parseTimestamp(createdStr)
	return &c, nil
}

func scanJailedUser(scanner scannable) (*JailedUser, error) {
	var j JailedUser
	var rolesJSON, createdStr string

	err := scanner.Scan(&j.GuildID, &j.UserID, &rolesJSON, &j.JailedBy, &j.Reason, &createdStr)
	if err != nil {
		return nil, err
	}

	j.CreatedAt = parseTimestamp(createdStr)

	if err := unmarshalStringSlice(rolesJSON, "jailed_user_roles", j.UserID, &j.Roles); err != nil {
		return nil, err
	}
	if j.Roles == nil {
		j.Roles = make([]string, 0)
	}

	return &j, nil
}

func (d *DB) CreateModCase(guildID, userID, modID, action, reason string) (int64, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `INSERT INTO moderation_cases (guild_id, user_id, mod_id, action, reason) VALUES (?, ?, ?, ?, ?)`
	res, err := d.conn.ExecContext(ctx, query, guildID, userID, modID, action, reason)
	if err != nil {
		return 0, fmt.Errorf("failed to create mod case: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) GetModCase(guildID string, caseID int64) (*ModCase, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM moderation_cases WHERE guild_id = ? AND case_id = ?`, modCaseSelectFields)
	row := d.conn.QueryRowContext(ctx, query, guildID, caseID)

	c, err := scanModCase(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get mod case %d: %w", caseID, err)
	}
	return c, nil
}

func (d *DB) GetUserModCases(guildID, userID string) ([]ModCase, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM moderation_cases WHERE guild_id = ? AND user_id = ? ORDER BY case_id DESC`, modCaseSelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query mod cases for user %s: %w", userID, err)
	}
	defer rows.Close()

	cases := make([]ModCase, 0)
	for rows.Next() {
		c, err := scanModCase(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan mod case: %w", err)
		}
		cases = append(cases, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating mod cases: %w", err)
	}
	return cases, nil
}

func (d *DB) DeleteModCase(guildID string, caseID int64) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM moderation_cases WHERE guild_id = ? AND case_id = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, caseID)
	if err != nil {
		return fmt.Errorf("failed to delete mod case %d: %w", caseID, err)
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

func (d *DB) UpdateModCaseReason(guildID string, caseID int64, newReason string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `UPDATE moderation_cases SET reason = ? WHERE guild_id = ? AND case_id = ?`
	res, err := d.conn.ExecContext(ctx, query, newReason, guildID, caseID)
	if err != nil {
		return fmt.Errorf("failed to update mod case %d: %w", caseID, err)
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

func (d *DB) DeleteAllModCases(guildID string) (int64, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM moderation_cases WHERE guild_id = ?`
	res, err := d.conn.ExecContext(ctx, query, guildID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete all mod cases for guild %s: %w", guildID, err)
	}
	return res.RowsAffected()
}

var ErrAlreadyJailed = errors.New("user is already jailed in this server")

func (d *DB) SaveJailedUser(guildID, userID string, roles []string, jailedBy, reason string) error {
	ctx, cancel := newCtx()
	defer cancel()

	if roles == nil {
		roles = make([]string, 0)
	}
	rolesBytes, err := marshalStringSlice(roles, "jailed_user_roles")
	if err != nil {
		return err
	}
	query := `
	INSERT INTO jailed_users (guild_id, user_id, roles_json, jailed_by, reason) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(guild_id, user_id) DO NOTHING;`
	res, err := d.conn.ExecContext(ctx, query, guildID, userID, string(rolesBytes), jailedBy, reason)
	if err != nil {
		return fmt.Errorf("failed to save jailed user %s: %w", userID, err)
	}
	rows, errRows := res.RowsAffected()
	if errRows == nil && rows == 0 {
		return ErrAlreadyJailed
	}
	return nil
}

func (d *DB) RemoveJailedUser(guildID, userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM jailed_users WHERE guild_id = ? AND user_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID); err != nil {
		return fmt.Errorf("failed to remove jailed user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) GetJailedUserInfo(guildID, userID string) (*JailedUser, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM jailed_users WHERE guild_id = ? AND user_id = ?`, jailedUserSelectFields)
	row := d.conn.QueryRowContext(ctx, query, guildID, userID)

	j, err := scanJailedUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get jailed user %s: %w", userID, err)
	}
	return j, nil
}

func (d *DB) IsUserJailed(guildID, userID string) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT 1 FROM jailed_users WHERE guild_id = ? AND user_id = ?`
	var exists int
	err := d.conn.QueryRowContext(ctx, query, guildID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check jail status for user %s: %w", userID, err)
	}
	return true, nil
}

func (d *DB) GetAllJailedUsers(guildID string) ([]JailedUser, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := fmt.Sprintf(`SELECT %s FROM jailed_users WHERE guild_id = ?`, jailedUserSelectFields)
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query jailed users for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	users := make([]JailedUser, 0)
	for rows.Next() {
		j, err := scanJailedUser(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan jailed user: %w", err)
		}
		users = append(users, *j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating jailed users: %w", err)
	}
	return users, nil
}

func (d *DB) LockUserNickname(guildID, userID, nickname, lockedBy string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO guild_locked_nicknames (guild_id, user_id, nickname, locked_by) VALUES (?, ?, ?, ?)
	ON CONFLICT(guild_id, user_id) DO UPDATE SET nickname = excluded.nickname, locked_by = excluded.locked_by, created_at = CURRENT_TIMESTAMP;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, nickname, lockedBy); err != nil {
		return fmt.Errorf("failed to lock nickname for user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) UnlockUserNickname(guildID, userID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM guild_locked_nicknames WHERE guild_id = ? AND user_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID); err != nil {
		return fmt.Errorf("failed to unlock nickname for user %s: %w", userID, err)
	}
	return nil
}

func (d *DB) GetLockedNickname(guildID, userID string) (string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT nickname FROM guild_locked_nicknames WHERE guild_id = ? AND user_id = ?`
	var nickname string
	err := d.conn.QueryRowContext(ctx, query, guildID, userID).Scan(&nickname)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get locked nickname for user %s: %w", userID, err)
	}
	return nickname, nil
}

func (d *DB) GetAllLockedNicknames(guildID string) (map[string]string, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT user_id, nickname FROM guild_locked_nicknames WHERE guild_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query locked nicknames for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	nicks := make(map[string]string)
	for rows.Next() {
		var uid, nick string
		if err := rows.Scan(&uid, &nick); err != nil {
			return nil, fmt.Errorf("failed to scan locked nickname: %w", err)
		}
		nicks[uid] = nick
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating locked nicknames: %w", err)
	}
	return nicks, nil
}

func (d *DB) SaveTempRole(guildID, userID, roleID string, expiresAt time.Time, assignedBy string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `
	INSERT INTO guild_temp_roles (guild_id, user_id, role_id, expires_at, assigned_by, created_at)
	VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(guild_id, user_id, role_id) DO UPDATE SET expires_at = excluded.expires_at, assigned_by = excluded.assigned_by;`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, roleID, expiresAt.UTC().Format("2006-01-02 15:04:05"), assignedBy); err != nil {
		return fmt.Errorf("failed to save temp role: %w", err)
	}
	return nil
}

func (d *DB) RemoveTempRole(guildID, userID, roleID string) error {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM guild_temp_roles WHERE guild_id = ? AND user_id = ? AND role_id = ?`
	if _, err := d.conn.ExecContext(ctx, query, guildID, userID, roleID); err != nil {
		return fmt.Errorf("failed to remove temp role: %w", err)
	}
	return nil
}

func (d *DB) DeleteExpiredTempRole(guildID, userID, roleID string, maxExpiresAt time.Time) (bool, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `DELETE FROM guild_temp_roles WHERE guild_id = ? AND user_id = ? AND role_id = ? AND expires_at <= ?`
	res, err := d.conn.ExecContext(ctx, query, guildID, userID, roleID, maxExpiresAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return false, fmt.Errorf("failed to delete expired temp role: %w", err)
	}
	rows, errRows := res.RowsAffected()
	if errRows != nil {
		return false, errRows
	}
	return rows > 0, nil
}

func (d *DB) GetExpiredTempRoles() ([]TempRoleEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, role_id, expires_at, assigned_by, created_at FROM guild_temp_roles WHERE expires_at <= datetime('now')`
	rows, err := d.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired temp roles: %w", err)
	}
	defer rows.Close()

	entries := make([]TempRoleEntry, 0)
	for rows.Next() {
		var e TempRoleEntry
		var expStr, crtStr string
		if err := rows.Scan(&e.GuildID, &e.UserID, &e.RoleID, &expStr, &e.AssignedBy, &crtStr); err != nil {
			return nil, fmt.Errorf("failed to scan temp role: %w", err)
		}
		e.ExpiresAt = parseTimestamp(expStr)
		e.CreatedAt = parseTimestamp(crtStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating expired temp roles: %w", err)
	}
	return entries, nil
}

func (d *DB) GetActiveTempRolesForUser(guildID, userID string) ([]TempRoleEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, role_id, expires_at, assigned_by, created_at FROM guild_temp_roles WHERE guild_id = ? AND user_id = ? AND expires_at > datetime('now')`
	rows, err := d.conn.QueryContext(ctx, query, guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active temp roles for user %s in guild %s: %w", userID, guildID, err)
	}
	defer rows.Close()

	entries := make([]TempRoleEntry, 0)
	for rows.Next() {
		var e TempRoleEntry
		var expStr, crtStr string
		if err := rows.Scan(&e.GuildID, &e.UserID, &e.RoleID, &expStr, &e.AssignedBy, &crtStr); err != nil {
			return nil, fmt.Errorf("failed to scan active temp role: %w", err)
		}
		e.ExpiresAt = parseTimestamp(expStr)
		e.CreatedAt = parseTimestamp(crtStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active temp roles: %w", err)
	}
	return entries, nil
}

func (d *DB) GetUserTempRoles(guildID, userID string) ([]TempRoleEntry, error) {
	ctx, cancel := newCtx()
	defer cancel()

	query := `SELECT guild_id, user_id, role_id, expires_at, assigned_by, created_at FROM guild_temp_roles WHERE guild_id = ? AND user_id = ?`
	rows, err := d.conn.QueryContext(ctx, query, guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query temp roles for user %s in guild %s: %w", userID, guildID, err)
	}
	defer rows.Close()

	entries := make([]TempRoleEntry, 0)
	for rows.Next() {
		var e TempRoleEntry
		var expStr, crtStr string
		if err := rows.Scan(&e.GuildID, &e.UserID, &e.RoleID, &expStr, &e.AssignedBy, &crtStr); err != nil {
			return nil, fmt.Errorf("failed to scan temp role: %w", err)
		}
		e.ExpiresAt = parseTimestamp(expStr)
		e.CreatedAt = parseTimestamp(crtStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating temp roles: %w", err)
	}
	return entries, nil
}
