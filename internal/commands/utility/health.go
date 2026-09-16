package utility

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

var startTime = time.Now()

type PingCmd struct{}

func (c *PingCmd) Name() string        { return "ping" }
func (c *PingCmd) Aliases() []string   { return []string{} }
func (c *PingCmd) Category() string    { return "Utility" }
func (c *PingCmd) Description() string { return "Measures current bot latency and database ping." }
func (c *PingCmd) Usage() string       { return "" }
func (c *PingCmd) Example() string     { return "" }

func (c *PingCmd) Execute(ctx *bot.Context) error {
	start := time.Now()
	msg, err := ctx.ReplyText("Pinging...")
	if err != nil {
		return err
	}

	rttMs := time.Since(start).Milliseconds()
	wsLatencyMs := ctx.Session.HeartbeatLatency().Milliseconds()

	dbPingStr := "N/A"
	if dbPing, err := ctx.DB.Ping(); err == nil {
		if dbPing < time.Millisecond {
			us := dbPing.Microseconds()
			if us < 1 {
				dbPingStr = "< 1 µs"
			} else {
				dbPingStr = fmt.Sprintf("%d µs", us)
			}
		} else {
			dbPingStr = fmt.Sprintf("%d ms", dbPing.Milliseconds())
		}
	}

	responseText := fmt.Sprintf("**Pong!** WebSocket Ping: `%d ms` | Message RTT: `%d ms` | Database Ping: `%s`", wsLatencyMs, rttMs, dbPingStr)
	_, err = ctx.Session.ChannelMessageEdit(msg.ChannelID, msg.ID, responseText)
	return err
}

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
)

type HealthCmd struct{}

func (c *HealthCmd) Name() string      { return "health" }
func (c *HealthCmd) Aliases() []string { return []string{"status", "sysinfo", "system"} }
func (c *HealthCmd) Category() string  { return "Utility" }
func (c *HealthCmd) Description() string {
	return "Displays system statistics, memory usage, uptime, and WebSocket latency."
}
func (c *HealthCmd) Usage() string   { return "" }
func (c *HealthCmd) Example() string { return "" }

func (c *HealthCmd) Execute(ctx *bot.Context) error {
	if !ctx.IsOwner() && !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
		return nil
	}
	cmdStart := time.Now()

	msg, err := ctx.ReplyText("Gathering system health metrics...")
	if err != nil {
		return err
	}

	metrics := collectHealthMetrics(ctx)
	rttMs := time.Since(cmdStart).Milliseconds()
	wsLatencyMs := ctx.Session.HeartbeatLatency().Milliseconds()

	embed := buildHealthEmbed(metrics, wsLatencyMs, rttMs)
	emptyContent := ""
	_, err = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:      msg.ID,
		Channel: msg.ChannelID,
		Content: &emptyContent,
		Embeds:  &[]*discordgo.MessageEmbed{embed},
	})
	if err != nil {
		return fmt.Errorf("failed to edit message with health dashboard: %w", err)
	}

	return nil
}

type healthMetrics struct {
	cpuPercent    float64
	rssMB         float64
	procIOStr     string
	allocMB       float64
	lastGCPauseMs float64
	numGC         uint32
	numGoroutine  int

	sysRAMStr string
	diskStr   string
	sysNetStr string
	loadStr   string

	dbPingStr    string
	dbStatsStr   string
	dbFileSizeMB string

	guildCount int
	userCount  int
}

func collectHealthMetrics(ctx *bot.Context) healthMetrics {
	var m healthMetrics
	m.cpuPercent, m.rssMB, m.procIOStr, m.allocMB, m.lastGCPauseMs, m.numGC, m.numGoroutine = collectProcessMetrics()
	m.sysRAMStr, m.diskStr, m.sysNetStr, m.loadStr = collectHostMetrics()
	m.dbPingStr, m.dbStatsStr, m.dbFileSizeMB = collectDBMetrics(ctx)
	m.guildCount, m.userCount = countSessionEntities(ctx)
	return m
}

func collectProcessMetrics() (cpuPercent, rssMB float64, procIOStr string, allocMB, lastGCPauseMs float64, numGC uint32, numGoroutine int) {
	pid := os.Getpid()
	p, errProc := process.NewProcess(int32(pid))

	if errProc == nil && p != nil {
		if times, errTimes := p.Times(); errTimes == nil {
			if createTime, errCreate := p.CreateTime(); errCreate == nil {
				elapsedSec := float64(time.Now().UnixMilli()-createTime) / 1000.0
				if elapsedSec > 0 {
					totalCPUSec := times.User + times.System
					cpuPercent = (totalCPUSec / elapsedSec) * 100.0
				}
			}
		}
		if memInfo, errMem := p.MemoryInfo(); errMem == nil && memInfo != nil {
			rssMB = float64(memInfo.RSS) / float64(MB)
		}
		if procIO, errProcIO := p.IOCounters(); errProcIO == nil && procIO != nil {
			procIOStr = fmt.Sprintf("`%s` | `%s`", helpers.FormatBytes(procIO.ReadBytes), helpers.FormatBytes(procIO.WriteBytes))
		}
	}
	if procIOStr == "" {
		procIOStr = "N/A"
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	mb := float64(MB)
	allocMB = float64(memStats.Alloc) / mb

	if memStats.NumGC > 0 {
		lastGCPauseMs = float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6
	}

	return cpuPercent, rssMB, procIOStr, allocMB, lastGCPauseMs, memStats.NumGC, runtime.NumGoroutine()
}

func collectHostMetrics() (sysRAMStr, diskStr, sysNetStr, loadStr string) {
	sysRAMStr = "N/A"
	if vMem, errMem := mem.VirtualMemory(); errMem == nil && vMem != nil {
		sysRAMStr = fmt.Sprintf("`%.1f%%`", vMem.UsedPercent)
	}

	diskStr = "N/A"
	if diskStat, errDisk := disk.Usage("."); errDisk == nil && diskStat != nil {
		diskStr = fmt.Sprintf("`%.1f%%`", diskStat.UsedPercent)
	}

	sysNetStr = "N/A"
	if netStats, errNet := net.IOCounters(false); errNet == nil && len(netStats) > 0 {
		sysNetStr = fmt.Sprintf("`%s` | `%s`", helpers.FormatBytes(netStats[0].BytesSent), helpers.FormatBytes(netStats[0].BytesRecv))
	}

	loadStr = "N/A"
	if avg, errAvg := load.Avg(); errAvg == nil && avg != nil {
		loadStr = fmt.Sprintf("`%.2f`, `%.2f`, `%.2f`", avg.Load1, avg.Load5, avg.Load15)
	}

	return sysRAMStr, diskStr, sysNetStr, loadStr
}

func collectDBMetrics(ctx *bot.Context) (dbPingStr, dbStatsStr, dbFileSizeMB string) {
	dbPingStr = "N/A"
	dbStatsStr = "Offline"
	dbFileSizeMB = "N/A"

	if dbPing, errPing := ctx.DB.Ping(); errPing == nil {
		if dbPing < time.Millisecond {
			us := dbPing.Microseconds()
			if us < 1 {
				dbPingStr = "< 1 µs"
			} else {
				dbPingStr = fmt.Sprintf("%d µs", us)
			}
		} else {
			dbPingStr = fmt.Sprintf("%d ms", dbPing.Milliseconds())
		}
	}

	stats := ctx.DB.Stats()
	dbStatsStr = fmt.Sprintf("`%d/%d` (Idle: `%d`)", stats.OpenConnections, stats.MaxOpenConnections, stats.Idle)

	dbPath := ""
	if ctx.Config != nil && ctx.Config.DatabasePath != "" {
		dbPath = ctx.Config.DatabasePath
	} else {
		dbPath = filepath.Join("data", "bot.db")
	}

	if fi, errStat := os.Stat(dbPath); errStat == nil && fi.Size() >= 0 {
		dbFileSizeMB = fmt.Sprintf("`%s`", helpers.FormatBytes(uint64(fi.Size())))
	}

	return dbPingStr, dbStatsStr, dbFileSizeMB
}

func countSessionEntities(ctx *bot.Context) (guildCount, userCount int) {
	if ctx.Session.State != nil {
		ctx.Session.State.RLock()
		guildCount = len(ctx.Session.State.Guilds)
		for _, g := range ctx.Session.State.Guilds {
			userCount += g.MemberCount
		}
		ctx.Session.State.RUnlock()
	}
	return guildCount, userCount
}

func buildHealthEmbed(m healthMetrics, wsLatencyMs, rttMs int64) *discordgo.MessageEmbed {
	uptimeStr := fmt.Sprintf("<t:%d:R>", startTime.Unix())

	return &discordgo.MessageEmbed{
		Title: "Status",
		Color: helpers.ColorDefault,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf(
				"OS: %s/%s · Go%s · DiscordGo %s",
				runtime.GOOS,
				runtime.GOARCH,
				strings.TrimPrefix(runtime.Version(), "go"),
				discordgo.VERSION,
			),
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Latency & Gateway",
				Value: fmt.Sprintf(
					"· WS Ping: `%d ms` \n· Message RTT: `%d ms` \n· DB Ping: `%s`",
					wsLatencyMs, rttMs, dbPingStrSafe(m.dbPingStr),
				),
				Inline: true,
			},
			{
				Name: "Bot Information",
				Value: fmt.Sprintf(
					"· Started: %s\n· Guilds: `%s` | Users: `%s`",
					uptimeStr, helpers.FormatNumber(m.guildCount), helpers.FormatNumber(m.userCount),
				),
				Inline: true,
			},
			{
				Name: "Host & System Load",
				Value: fmt.Sprintf(
					"· CPU (1m|5m|15m): %s\n· RAM Used: %s | Disk Used: %s\n· RX/TX: %s",
					m.loadStr, m.sysRAMStr, m.diskStr, m.sysNetStr,
				),
				Inline: false,
			},
			{
				Name: "Bot Process",
				Value: fmt.Sprintf(
					"· CPU Usage: `%.2f%%` \n"+
						"· Goroutines: `%d`\n"+
						"· Heap Alloc : `%.2f MB`\n"+
						"· RSS: `%.2f MB` \n"+
						"· GC Pause: `%.2f ms` (`%d` cycle(s))\n"+
						"· Disk R/W: %s\n"+
						"· Database: %s (Conns: %s)",
					m.cpuPercent, m.numGoroutine,
					m.allocMB, m.rssMB, m.lastGCPauseMs, m.numGC,
					m.procIOStr, m.dbFileSizeMB, m.dbStatsStr,
				),
				Inline: false,
			},
		},
	}
}

func dbPingStrSafe(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
