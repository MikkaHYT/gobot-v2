package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gobot/internal/logger"

	_ "modernc.org/sqlite"
)

var QueryTimeout = 10 * time.Second

var ErrNotFound = errors.New("record not found")

type DB struct {
	conn  *sql.DB
	Cache *ConfigCache
}

func InitDBWithOptions(dbPath string, maxConns int, busyTimeoutMs int) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	if busyTimeoutMs <= 0 {
		busyTimeoutMs = 10000
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-8000)&_txlock=immediate", filepath.ToSlash(dbPath), busyTimeoutMs)

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	var initSuccess bool
	defer func() {
		if !initSuccess {
			_ = conn.Close()
		}
	}()

	if maxConns <= 0 {
		maxConns = 10
	}
	conn.SetMaxOpenConns(maxConns)
	conn.SetMaxIdleConns(maxConns)
	conn.SetConnMaxIdleTime(15 * time.Minute)

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()
	if err := conn.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("database connectivity check failed: %w", err)
	}

	if err := runMigrations(conn); err != nil {
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	indicesSchema := `
	CREATE INDEX IF NOT EXISTS idx_mod_cases_guild_user ON moderation_cases(guild_id, user_id);
	CREATE INDEX IF NOT EXISTS idx_giveaways_guild_status ON giveaways(guild_id, status);
	CREATE INDEX IF NOT EXISTS idx_giveaway_participants_id ON giveaway_participants(giveaway_id);
	CREATE INDEX IF NOT EXISTS idx_user_xp_guild_xp ON user_xp(guild_id, xp DESC);
	CREATE INDEX IF NOT EXISTS idx_temp_roles_expires ON guild_temp_roles(expires_at);
	CREATE INDEX IF NOT EXISTS idx_reaction_roles_guild ON reaction_roles(guild_id);
	CREATE INDEX IF NOT EXISTS idx_tags_guild ON tags(guild_id);
	`

	if _, err := conn.Exec(indicesSchema); err != nil {
		return nil, fmt.Errorf("failed to create database indices: %w", err)
	}

	initSuccess = true
	return &DB{conn: conn, Cache: NewConfigCache(5 * time.Minute)}, nil
}

func (d *DB) Close() error {
	if d.conn != nil {
		if _, err := d.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
			logger.Warnf("[DATABASE] WAL truncate checkpoint during shutdown returned error: %v", err)
		}
		return d.conn.Close()
	}
	return nil
}

func newCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), QueryTimeout)
}

func parseTimestamp(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}

	digitsOnly := true
	for _, r := range raw {
		if r < '0' || r > '9' {
			digitsOnly = false
			break
		}
	}
	if digitsOnly {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if len(raw) >= 13 {
				return time.UnixMilli(n).UTC()
			}
			return time.Unix(n, 0).UTC()
		}
	}

	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.UTC()
		}
	}
	logger.Warnf("[DATABASE] parseTimestamp: unable to parse unrecognized timestamp format %q", raw)
	return time.Time{}
}

func unmarshalStringSlice(data, field, id string, dest *[]string) error {
	if data == "" || data == "[]" {
		*dest = make([]string, 0)
		return nil
	}
	if err := json.Unmarshal([]byte(data), dest); err != nil {
		return fmt.Errorf("failed to unmarshal %s for %s: %w", field, id, err)
	}
	if *dest == nil {
		*dest = make([]string, 0)
	}
	return nil
}

func marshalStringSlice(data []string, field string) ([]byte, error) {
	if data == nil {
		data = []string{}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s: %w", field, err)
	}
	return b, nil
}

func VerifyBackup(backupPath string) error {
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=query_only(ON)&_pragma=busy_timeout(5000)", filepath.ToSlash(backupPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open backup database for verification: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var integrityResult string
	row := db.QueryRowContext(ctx, "PRAGMA integrity_check(1);")
	if err := row.Scan(&integrityResult); err != nil {
		return fmt.Errorf("backup integrity check query failed: %w", err)
	}
	if !strings.EqualFold(integrityResult, "ok") {
		return fmt.Errorf("backup integrity check failed: %s", integrityResult)
	}

	var tableCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table';").Scan(&tableCount); err != nil {
		return fmt.Errorf("backup schema verification failed: %w", err)
	}

	return nil
}

func (d *DB) BackupDatabase(dbPath, backupDir string, retentionCount int) (string, error) {
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup dir: %w", err)
	}

	timestamp := time.Now().UTC().Format("2006-01-02_150405.000")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("bot_backup_%s.db", timestamp))

	ctx, cancel := newCtx()
	defer cancel()

	query := "VACUUM INTO ?;"
	if _, err := d.conn.ExecContext(ctx, query, backupFile); err != nil {
		return "", fmt.Errorf("failed to execute database backup: %w", err)
	}

	if err := VerifyBackup(backupFile); err != nil {
		_ = os.Remove(backupFile)
		return "", fmt.Errorf("newly created backup failed verification: %w", err)
	}

	if retentionCount > 0 {
		if errPrune := pruneOldBackups(backupDir, retentionCount); errPrune != nil {
			logger.Warnf("[BACKUP] Failed to prune old database backups: %v", errPrune)
		}
	}

	return backupFile, nil
}

func isBackupFile(name string) bool {
	return strings.HasSuffix(name, ".db") && strings.HasPrefix(name, "bot_backup_")
}

func (d *DB) AutoBackupIfDue(dbPath, backupDir string, interval time.Duration, retentionCount int) (string, bool, error) {
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(dbPath), "backups")
	}

	if interval <= 0 {
		interval = 24 * time.Hour
	}

	if entries, err := os.ReadDir(backupDir); err == nil {
		var newestModTime time.Time
		for _, entry := range entries {
			if entry.IsDir() || !isBackupFile(entry.Name()) {
				continue
			}
			info, errInfo := entry.Info()
			if errInfo == nil {
				if info.ModTime().After(newestModTime) {
					newestModTime = info.ModTime()
				}
			}
		}

		if !newestModTime.IsZero() && time.Since(newestModTime) < interval {
			return "", false, nil
		}
	}

	backupFile, err := d.BackupDatabase(dbPath, backupDir, retentionCount)
	if err != nil {
		return "", false, err
	}
	return backupFile, true, nil
}

func pruneOldBackups(backupDir string, retentionCount int) error {
	if retentionCount <= 0 {
		return nil
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}

	type backupInfo struct {
		path    string
		modTime time.Time
	}

	var backups []backupInfo
	for _, entry := range entries {
		if entry.IsDir() || !isBackupFile(entry.Name()) {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo == nil {
			backups = append(backups, backupInfo{
				path:    filepath.Join(backupDir, entry.Name()),
				modTime: info.ModTime(),
			})
		}
	}

	if len(backups) <= retentionCount {
		return nil
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.Before(backups[j].modTime)
	})

	toDelete := len(backups) - retentionCount
	for i := 0; i < toDelete; i++ {
		if err := os.Remove(backups[i].path); err != nil && !os.IsNotExist(err) {
			logger.Warnf("[DATABASE] Failed to prune expired backup %s: %v", backups[i].path, err)
		}
	}

	return nil
}

func (d *DB) Ping() (time.Duration, error) {
	if d == nil || d.conn == nil {
		return 0, errors.New("database connection is nil")
	}
	start := time.Now()
	ctx, cancel := newCtx()
	defer cancel()
	if err := d.conn.PingContext(ctx); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func (d *DB) Stats() sql.DBStats {
	if d == nil || d.conn == nil {
		return sql.DBStats{}
	}
	return d.conn.Stats()
}
