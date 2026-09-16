package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/joho/godotenv"
)

var ErrDiscordTokenMissing = errors.New("DISCORD_TOKEN is not set")

type Config struct {
	DiscordToken           string
	Prefix                 string
	DataDir                string
	DatabasePath           string
	OwnerIDs               []string
	YTDLPCookiesPath       string
	LogLevel               string
	DefaultEmbedColor      int
	MaxGiveawayDuration    time.Duration
	MaxSnipeLimit          int
	JuiceWRLDAPIURL        string
	JuiceWRLDAPITimeout    time.Duration
	DBMaxOpenConns         int
	DBBusyTimeout          int
	AutoBackupEnabled      bool
	AutoBackupInterval     time.Duration
	BackupRetentionCount   int
	ConsoleWebhookURL      string
	GlobalBlacklistedUsers []string
	GlobalDisabledCommands []string
	CustomUserAgent        string
	DefaultRankCardTheme   string
	CustomFontPath         string
	MaxMediaDownloadSizeMB int64
	TempDownloadDir        string
	SnipeExpirationHours   int
	OCRSpaceAPIKey         string
	LastFmAPIKey           string
	RadioCacheDir          string
	PrimaryGuildID         string
	StorageToAPIKey        string
	DefaultRadioCoverURL   string
}

func DefaultConfig() *Config {
	dataDir := "data"
	return &Config{
		Prefix:                 ",",
		DataDir:                dataDir,
		DatabasePath:           filepath.Join(dataDir, "bot.db"),
		LogLevel:               "info",
		DefaultEmbedColor:      helpers.ColorDefault,
		MaxGiveawayDuration:    365 * 24 * time.Hour,
		MaxSnipeLimit:          1000,
		SnipeExpirationHours:   24,
		JuiceWRLDAPIURL:        "https://juicewrldapi.com",
		JuiceWRLDAPITimeout:    8 * time.Second,
		DBMaxOpenConns:         10,
		DBBusyTimeout:          5000,
		AutoBackupEnabled:      true,
		AutoBackupInterval:     24 * time.Hour,
		BackupRetentionCount:   7,
		CustomUserAgent:        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36 Edg/134.0.0.0",
		DefaultRankCardTheme:   "dark",
		CustomFontPath:         filepath.Join("assets", "fonts", "Outfit.ttf"),
		MaxMediaDownloadSizeMB: 500,
		TempDownloadDir:        filepath.Join(dataDir, "temp"),
		RadioCacheDir:          filepath.Join(dataDir, "cache", "songs"),
		YTDLPCookiesPath:       filepath.Join(dataDir, "cookies.txt"),
		OCRSpaceAPIKey:         "helloworld",
		DefaultRadioCoverURL:   helpers.DefaultRadioCoverURL,
	}
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Error reading .env file: %v\n", err)
	}
	return LoadConfigWithEnv(os.Getenv)
}

func LoadConfigWithEnv(getenv func(string) string) (*Config, error) {
	get := func(k string) string {
		return strings.TrimSpace(getenv(k))
	}

	token := get("DISCORD_TOKEN")
	if token == "" {
		return nil, ErrDiscordTokenMissing
	}

	cfg := DefaultConfig()
	cfg.DiscordToken = token

	if v := get("BOT_PREFIX"); v != "" {
		if len(v) <= 10 {
			cfg.Prefix = v
		} else {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] BOT_PREFIX=%q exceeds 10 characters. Retaining default %q.\n", v, cfg.Prefix)
		}
	}

	if v := get("DATA_DIR"); v != "" {
		cfg.DataDir = v
		cfg.DatabasePath = filepath.Join(v, "bot.db")
		cfg.TempDownloadDir = filepath.Join(v, "temp")
		cfg.RadioCacheDir = filepath.Join(v, "cache", "songs")
		cfg.YTDLPCookiesPath = filepath.Join(v, "cookies.txt")
	}

	if v := get("DATABASE_PATH"); v != "" {
		cfg.DatabasePath = v
	} else if v := get("DB_PATH"); v != "" {
		cfg.DatabasePath = v
	}

	if v := get("RADIO_CACHE_DIR"); v != "" {
		cfg.RadioCacheDir = v
	}
	if v := get("TEMP_DOWNLOAD_DIR"); v != "" {
		cfg.TempDownloadDir = v
	}
	if v := get("YTDLP_COOKIES_PATH"); v != "" {
		cfg.YTDLPCookiesPath = v
	}
	if v := get("CUSTOM_FONT_PATH"); v != "" {
		cfg.CustomFontPath = v
	}

	cfg.OwnerIDs = parseSlice(get("BOT_OWNERS"))

	if v := strings.ToLower(get("LOG_LEVEL")); v != "" {
		switch v {
		case "debug", "info", "warn", "warning", "error":
			cfg.LogLevel = v
		default:
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid LOG_LEVEL=%q. Retaining default %q.\n", v, cfg.LogLevel)
		}
	}

	if colorStr := get("DEFAULT_EMBED_COLOR"); colorStr != "" {
		clean := strings.TrimPrefix(strings.TrimPrefix(colorStr, "#"), "0x")
		if val, err := strconv.ParseInt(clean, 16, 64); err == nil && val >= 0 && val <= 0xFFFFFF {
			cfg.DefaultEmbedColor = int(val)
		} else {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid DEFAULT_EMBED_COLOR=%q. Retaining default 0x%06X.\n", colorStr, cfg.DefaultEmbedColor)
		}
	}

	cfg.MaxGiveawayDuration = envDurationBounded(get, "MAX_GIVEAWAY_DURATION", cfg.MaxGiveawayDuration, 10*time.Second, 365*24*time.Hour)
	cfg.MaxSnipeLimit = envIntBounded(get, "MAX_SNIPE_LIMIT", cfg.MaxSnipeLimit, 1, 10000)
	cfg.SnipeExpirationHours = envIntBounded(get, "SNIPE_EXPIRATION_HOURS", cfg.SnipeExpirationHours, 1, 720)

	if v := get("JUICEWRLD_API_URL"); v != "" {
		cfg.JuiceWRLDAPIURL = v
	}
	cfg.JuiceWRLDAPITimeout = envDurationBounded(get, "JUICEWRLD_API_TIMEOUT", cfg.JuiceWRLDAPITimeout, 500*time.Millisecond, 60*time.Second)

	cfg.DBMaxOpenConns = envIntBounded(get, "DB_MAX_OPEN_CONNS", cfg.DBMaxOpenConns, 1, 100)

	if rawBusy := get("DB_BUSY_TIMEOUT"); rawBusy != "" {
		var ms int
		if d, err := parseDuration(rawBusy); err == nil && d > 0 {
			ms = int(d.Milliseconds())
		} else if val, err := strconv.Atoi(rawBusy); err == nil && val > 0 {
			ms = val
		}

		if ms > 0 {
			cfg.DBBusyTimeout = clampInt("DB_BUSY_TIMEOUT", ms, 500, 60000)
		} else {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid DB_BUSY_TIMEOUT=%q. Retaining default %dms.\n", rawBusy, cfg.DBBusyTimeout)
		}
	}

	if backupStr := get("AUTO_BACKUP_ENABLED"); backupStr != "" {
		if val, err := strconv.ParseBool(backupStr); err == nil {
			cfg.AutoBackupEnabled = val
		} else {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid AUTO_BACKUP_ENABLED=%q. Retaining default %t.\n", backupStr, cfg.AutoBackupEnabled)
		}
	}
	cfg.AutoBackupInterval = envDurationBounded(get, "AUTO_BACKUP_INTERVAL", cfg.AutoBackupInterval, 5*time.Minute, 30*24*time.Hour)

	if raw := get("BACKUP_RETENTION_COUNT"); raw != "" {
		cfg.BackupRetentionCount = envIntBounded(get, "BACKUP_RETENTION_COUNT", cfg.BackupRetentionCount, 1, 100)
	} else if rawOld := get("MAX_BACKUPS_RETAINED"); rawOld != "" {
		cfg.BackupRetentionCount = envIntBounded(get, "MAX_BACKUPS_RETAINED", cfg.BackupRetentionCount, 1, 100)
	}

	if v := get("CUSTOM_USER_AGENT"); v != "" {
		cfg.CustomUserAgent = v
	}
	helpers.SetUserAgent(cfg.CustomUserAgent)
	logger.SetUserAgent(cfg.CustomUserAgent)
	helpers.ColorDefault = cfg.DefaultEmbedColor
	if v := get("DEFAULT_RADIO_COVER_URL"); v != "" {
		cfg.DefaultRadioCoverURL = v
	}
	helpers.DefaultRadioCoverURL = cfg.DefaultRadioCoverURL

	if v := strings.ToLower(get("DEFAULT_RANK_CARD_THEME")); v != "" {
		cfg.DefaultRankCardTheme = v
	}

	cfg.MaxMediaDownloadSizeMB = int64(envIntBounded(get, "MAX_MEDIA_DOWNLOAD_SIZE_MB", int(cfg.MaxMediaDownloadSizeMB), 1, 5000))

	cfg.ConsoleWebhookURL = get("CONSOLE_WEBHOOK_URL")
	cfg.GlobalBlacklistedUsers = parseSlice(get("GLOBAL_BLACKLISTED_USERS"))
	cfg.GlobalDisabledCommands = parseSlice(get("GLOBAL_DISABLED_COMMANDS"))

	if v := get("OCR_SPACE_API_KEY"); v != "" {
		cfg.OCRSpaceAPIKey = v
	}
	cfg.LastFmAPIKey = get("LASTFM_API_KEY")
	cfg.PrimaryGuildID = get("PRIMARY_GUILD_ID")

	if v := get("STORAGE_TO_API_KEY"); v != "" {
		cfg.StorageToAPIKey = v
	} else if v := get("STORAGE_TO_TOKEN"); v != "" {
		cfg.StorageToAPIKey = v
	}

	return cfg, nil
}

func envIntBounded(get func(string) string, key string, def, minVal, maxVal int) int {
	raw := get(key)
	if raw == "" {
		return def
	}

	val, err := strconv.Atoi(raw)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(raw, "-") && maxVal > 0 {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%q exceeds integer range. Setting to %d.\n", key, raw, maxVal)
			return maxVal
		}
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid integer for %s=%q. Retaining default %d.\n", key, raw, def)
		return def
	}

	return clampInt(key, val, minVal, maxVal)
}

func clampInt(key string, val, minVal, maxVal int) int {
	if val < minVal {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%d is below minimum (%d). Setting to %d.\n", key, val, minVal, minVal)
		return minVal
	}
	if maxVal > 0 && val > maxVal {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%d exceeds maximum (%d). Setting to %d.\n", key, val, maxVal, maxVal)
		return maxVal
	}
	return val
}

func envDurationBounded(get func(string) string, key string, def, minVal, maxVal time.Duration) time.Duration {
	raw := get(key)
	if raw == "" {
		return def
	}

	d, err := parseDuration(raw)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(raw, "-") && maxVal > 0 {
			fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%q exceeds duration capacity. Setting to %v.\n", key, raw, maxVal)
			return maxVal
		}
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] Invalid duration for %s=%q. Retaining default %v.\n", key, raw, def)
		return def
	}

	if minVal >= 0 && d < 0 {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%v cannot be negative. Retaining default %v.\n", key, d, def)
		return def
	}
	if d < minVal {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%v is below minimum (%v). Setting to %v.\n", key, d, minVal, minVal)
		return minVal
	}
	if maxVal > 0 && d > maxVal {
		fmt.Fprintf(os.Stderr, "[CONFIG WARNING] %s=%v exceeds maximum (%v). Setting to %v.\n", key, d, maxVal, maxVal)
		return maxVal
	}
	return d
}

func parseSlice(val string) []string {
	if val == "" {
		return nil
	}
	parts := strings.FieldsFunc(val, func(r rune) bool {
		return r == ',' || r == ';'
	})
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

func parseDuration(s string) (time.Duration, error) {
	return helpers.ParseDuration(s)
}
