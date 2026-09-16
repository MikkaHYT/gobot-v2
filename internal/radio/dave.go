package radio

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/disgoorg/godave"
	"github.com/gorilla/websocket"
	davesession "github.com/thomas-vilte/dave-go/session"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/nacl/secretbox"
)

type DAVEPort struct {
	session     *discordgo.Session
	onVoiceLoss func(connection VoiceConnection, guildID, channelID string)
}

func NewDAVEPort(session *discordgo.Session, onVoiceLoss func(connection VoiceConnection, guildID, channelID string)) *DAVEPort {
	return &DAVEPort{
		session:     session,
		onVoiceLoss: onVoiceLoss,
	}
}

func (p *DAVEPort) Join(ctx context.Context, guildID, channelID string) (VoiceConnection, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if p.session == nil {
		return nil, ErrMissingContext
	}
	if ctx == nil {
		return nil, ErrMissingContext
	}
	conn, err := ConnectDAVEVoiceContext(ctx, p.session, guildID, channelID, p.onVoiceLoss)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (p *DAVEPort) Disconnect(ctx context.Context, guildID string) error {
	if p.session == nil {
		return nil
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return p.session.ChannelVoiceJoinManual(guildID, "", false, true)
}

type DAVEVoiceConn struct {
	mu                    sync.RWMutex
	daveMu                sync.Mutex
	session               *discordgo.Session
	guildID               string
	channelID             string
	userID                string
	sessionID             string
	token                 string
	endpoint              string
	ssrc                  uint32
	secretKey             [32]byte
	selectedMode          string
	wsConn                *websocket.Conn
	wsWriteMu             sync.Mutex
	udpConn               *net.UDPConn
	targetUDPAddr         *net.UDPAddr
	readyChan             chan struct{}
	closeChan             chan struct{}
	isClosed              bool
	isEstablished         bool
	sequence              uint16
	timestamp             uint32
	nonceCounter          uint32
	daveSess              *davesession.Session
	serverTransitionID    uint16
	lastSeq               int64
	speaking              bool
	speakingSet           bool
	speakingMu            sync.Mutex
	invalidCommitAttempts atomic.Int32
	lastInvalidCommitTime atomic.Int64
	voiceLossOnce         sync.Once
	removeHandlers        []func()
	onVoiceLoss           func(connection VoiceConnection, guildID, channelID string)
	heartbeatsMu          sync.Mutex
	outstandingHeartbeats map[int64]time.Time
	heartbeatInterval     time.Duration
	remoteUsers           map[string]struct{}
	wg                    sync.WaitGroup
	workerMu              sync.Mutex
}

type daveCallbacks struct {
	conn *DAVEVoiceConn
}

func (c *DAVEVoiceConn) setupState() (endpoint, sessionID, token, userID string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endpoint, c.sessionID, c.token, c.userID
}

func (c *daveCallbacks) SendMLSKeyPackage(mlsKeyPackage []byte) error {
	if c.conn.IsClosed() {
		return fmt.Errorf("voice connection closed")
	}
	slog.Info("DAVE sending MLS key package (opcode 26)", "guild_id", c.conn.guildID, "bytes", len(mlsKeyPackage))
	buf := make([]byte, 1+len(mlsKeyPackage))
	buf[0] = 26
	copy(buf[1:], mlsKeyPackage)
	return c.conn.writeBinaryMessage(buf)
}

func (c *daveCallbacks) SendMLSCommitWelcome(mlsCommitWelcome []byte) error {
	if c.conn.IsClosed() {
		return fmt.Errorf("voice connection closed")
	}
	slog.Info("DAVE sending MLS commit/welcome (opcode 28)", "guild_id", c.conn.guildID, "bytes", len(mlsCommitWelcome))
	buf := make([]byte, 1+len(mlsCommitWelcome))
	buf[0] = 28
	copy(buf[1:], mlsCommitWelcome)
	return c.conn.writeBinaryMessage(buf)
}

func (c *daveCallbacks) SendReadyForTransition(transitionID uint16) error {
	if c.conn.IsClosed() {
		return fmt.Errorf("voice connection closed")
	}
	slog.Info("DAVE sending ready for transition (opcode 23)", "guild_id", c.conn.guildID, "transition_id", transitionID)
	c.conn.invalidCommitAttempts.Store(0)
	return c.conn.sendWSOpcode(23, map[string]any{
		"transition_id": transitionID,
	})
}

func (c *daveCallbacks) SendInvalidCommitWelcome(transitionID uint16) error {
	if c.conn.IsClosed() {
		return fmt.Errorf("voice connection closed")
	}
	now := time.Now().UnixNano()
	prev := c.conn.lastInvalidCommitTime.Swap(now)
	if prev > 0 && time.Duration(now-prev) > 30*time.Second {
		c.conn.invalidCommitAttempts.Store(1)
	} else {
		c.conn.invalidCommitAttempts.Add(1)
	}
	attempts := c.conn.invalidCommitAttempts.Load()
	slog.Warn("DAVE MLS transition failed: invalid commit/welcome",
		"guild_id", c.conn.guildID,
		"transition_id", transitionID,
		"attempts", attempts,
	)
	if attempts >= 3 {
		slog.Warn("DAVE MLS transition failure threshold reached, triggering voice reconnect",
			"guild_id", c.conn.guildID,
			"attempts", attempts,
		)
		c.conn.triggerVoiceLoss()
	}
	return c.conn.sendWSOpcode(31, map[string]any{
		"transition_id": transitionID,
	})
}

func (c *DAVEVoiceConn) triggerVoiceLoss() {
	if c == nil {
		return
	}
	c.voiceLossOnce.Do(func() {
		callback := c.onVoiceLoss
		c.startWorker(func() {
			c.Close()
			if callback != nil {
				callback(c, c.guildID, c.channelID)
			}
		})
	})
}

func (c *DAVEVoiceConn) startWorker(worker func()) bool {
	if worker == nil || c == nil {
		return false
	}
	c.workerMu.Lock()
	defer c.workerMu.Unlock()
	if c.IsClosed() {
		return false
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic("DAVE worker", recovered)
				if !c.IsClosed() {
					c.triggerVoiceLoss()
				}
			}
		}()
		worker()
	}()
	return true
}

func (c *DAVEVoiceConn) Wait(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		return ErrMissingContext
	}
	select {
	case <-c.closeChan:
	default:
		return fmt.Errorf("voice connection is still open")
	}
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ConnectDAVEVoiceContext(ctx context.Context, s *discordgo.Session, guildID, channelID string, onVoiceLoss func(connection VoiceConnection, guildID, channelID string)) (*DAVEVoiceConn, error) {
	if ctx == nil || s == nil {
		return nil, ErrMissingContext
	}
	if guildID == "" || channelID == "" {
		return nil, ErrInvalidRequest
	}

	slog.Info("DAVE voice connecting to channel", "guild_id", guildID, "channel_id", channelID)
	botUserID := resolveBotUserID(s)
	conn, err := newDAVEVoiceConn(s, guildID, channelID, botUserID, onVoiceLoss)
	if err != nil {
		return nil, err
	}
	setupStarted := time.Now()

	initDaveSessionVoiceStates(conn, s, guildID, channelID)

	if err := performGatewayHandshake(ctx, s, conn, guildID, channelID, setupStarted); err != nil {
		return nil, err
	}

	endpointValue, sessionID, token, userID := conn.setupState()
	if endpointValue == "" {
		conn.Close()
		return nil, fmt.Errorf("received empty voice server endpoint")
	}

	slog.Info("DAVE dialing voice gateway websocket", "guild_id", guildID, "endpoint", endpointValue)
	ws, err := dialVoiceGateway(ctx, endpointValue)
	if err != nil {
		conn.Close()
		return nil, err
	}

	conn.mu.Lock()
	if conn.isClosed {
		conn.mu.Unlock()
		_ = ws.Close()
		return nil, fmt.Errorf("voice connection closed during setup")
	}
	conn.wsConn = ws
	conn.mu.Unlock()

	conn.startWorker(conn.readLoop)

	if err := sendVoiceIdentify(conn, guildID, userID, sessionID, token); err != nil {
		conn.Close()
		return nil, err
	}

	if err := awaitVoiceReady(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}

	slog.Info("DAVE voice connection established and ready", "guild_id", guildID, "channel_id", channelID)
	return conn, nil
}

func resolveBotUserID(s *discordgo.Session) string {
	if s.State != nil && s.State.User != nil && s.State.User.ID != "" {
		return s.State.User.ID
	}
	if me, err := s.User("@me"); err == nil && me != nil {
		return me.ID
	}
	return ""
}

func newDAVEVoiceConn(s *discordgo.Session, guildID, channelID, botUserID string, onVoiceLoss func(connection VoiceConnection, guildID, channelID string)) (*DAVEVoiceConn, error) {
	cid, err := strconv.ParseUint(channelID, 10, 64)
	if err != nil || cid == 0 {
		return nil, fmt.Errorf("%w: invalid voice channel snowflake %q", ErrInvalidRequest, channelID)
	}
	conn := &DAVEVoiceConn{
		session:               s,
		guildID:               guildID,
		channelID:             channelID,
		userID:                botUserID,
		serverTransitionID:    0,
		lastSeq:               -1,
		closeChan:             make(chan struct{}),
		readyChan:             make(chan struct{}),
		onVoiceLoss:           onVoiceLoss,
		outstandingHeartbeats: make(map[int64]time.Time),
		remoteUsers:           make(map[string]struct{}),
	}
	cb := &daveCallbacks{conn: conn}
	conn.daveSess = davesession.New(godave.UserID(conn.userID), cb, davesession.WithLogger(slog.Default()))
	conn.daveSess.SetChannelID(godave.ChannelID(cid))
	return conn, nil
}

func (c *DAVEVoiceConn) trackRemoteUserLocked(userID string, present bool) {
	if c.remoteUsers == nil {
		c.remoteUsers = make(map[string]struct{})
	}
	if present {
		c.remoteUsers[userID] = struct{}{}
	} else {
		delete(c.remoteUsers, userID)
	}
}

func (c *DAVEVoiceConn) isAloneLocked() bool {
	return len(c.remoteUsers) == 0
}

func initDaveSessionVoiceStates(conn *DAVEVoiceConn, s *discordgo.Session, guildID, channelID string) {
	if conn.userID != "" {
		conn.withDave(func(ds *davesession.Session) {
			ds.AddUser(godave.UserID(conn.userID))
		})
	}

	var voiceStates []*discordgo.VoiceState
	if guild, errG := s.Guild(guildID); errG == nil && guild != nil {
		voiceStates = guild.VoiceStates
	} else if s.State != nil {
		if guild, errG := s.State.Guild(guildID); errG == nil && guild != nil {
			voiceStates = guild.VoiceStates
		}
	}
	for _, vs := range voiceStates {
		if vs.ChannelID == channelID {
			if vs.UserID != conn.userID {
				conn.mu.Lock()
				conn.trackRemoteUserLocked(vs.UserID, true)
				conn.mu.Unlock()
			}
			conn.withDave(func(ds *davesession.Session) {
				ds.AddUser(godave.UserID(vs.UserID))
			})
		}
	}
}

func performGatewayHandshake(ctx context.Context, s *discordgo.Session, conn *DAVEVoiceConn, guildID, channelID string, setupStarted time.Time) error {
	serverUpdateChan := make(chan *discordgo.VoiceServerUpdate, 1)
	stateUpdateChan := make(chan *discordgo.VoiceStateUpdate, 1)

	removeServerHandler := s.AddHandler(func(sess *discordgo.Session, v *discordgo.VoiceServerUpdate) {
		conn.handleVoiceServerUpdate(v, serverUpdateChan)
	})
	removeStateHandler := s.AddHandler(func(sess *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		conn.handleVoiceStateUpdate(v, stateUpdateChan)
	})

	conn.mu.Lock()
	conn.removeHandlers = append(conn.removeHandlers, removeServerHandler, removeStateHandler)
	conn.mu.Unlock()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	_ = s.ChannelVoiceJoinManual(guildID, "", false, true)
	setupTimer := time.NewTimer(250 * time.Millisecond)
	select {
	case <-setupCtx.Done():
		setupTimer.Stop()
		conn.Close()
		return setupCtx.Err()
	case <-setupTimer.C:
	}

	if err := s.ChannelVoiceJoinManual(guildID, channelID, false, true); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send voice state update: %w", err)
	}

	nudgeTicker := time.NewTicker(gatewaySetupNudgeInterval)
	defer nudgeTicker.Stop()

	setupState, setupErr := waitForGatewaySetup(setupCtx, gatewaySetupState{}, serverUpdateChan, stateUpdateChan, nudgeTicker.C, func() error {
		return s.ChannelVoiceJoinManual(guildID, channelID, false, true)
	})
	if setupErr != nil {
		if ctx.Err() != nil {
			conn.Close()
			return ctx.Err()
		}
		logGatewaySetupDiagnostic(guildID, channelID, voiceReconnectEpoch(ctx), setupStarted, setupState, "voice_setup_timeout")
		conn.Close()
		return fmt.Errorf("timed out waiting for Gateway voice server/state updates")
	}

	conn.mu.Lock()
	conn.endpoint = setupState.endpoint
	conn.token = setupState.token
	conn.sessionID = setupState.sessionID
	conn.mu.Unlock()
	return nil
}

func dialVoiceGateway(ctx context.Context, endpointValue string) (*websocket.Conn, error) {
	wsEndpoint := fmt.Sprintf("wss://%s?v=8", strings.TrimSuffix(endpointValue, ":80"))
	var ws *websocket.Conn
	var dialErr error
	for attempt := 0; attempt < 3; attempt++ {
		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
			NetDial: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
		}
		ws, _, dialErr = dialer.DialContext(ctx, wsEndpoint, nil)
		if dialErr == nil {
			break
		}
		dialTimer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			dialTimer.Stop()
			return nil, ctx.Err()
		case <-dialTimer.C:
		}
	}
	if dialErr != nil {
		return nil, fmt.Errorf("failed to connect to Voice Gateway at %s: %w", wsEndpoint, dialErr)
	}
	return ws, nil
}

func sendVoiceIdentify(conn *DAVEVoiceConn, guildID, userID, sessionID, token string) error {
	identify := map[string]any{
		"server_id":                 guildID,
		"user_id":                   userID,
		"session_id":                sessionID,
		"token":                     token,
		"video":                     false,
		"max_dave_protocol_version": 1,
		"streams": []map[string]any{
			{"type": "audio", "rid": "100", "quality": 100},
		},
	}
	if err := conn.sendWSOpcode(0, identify); err != nil {
		return fmt.Errorf("failed to send Identify (Opcode 0): %w", err)
	}
	return nil
}

func daveReadyTimeout() time.Duration {
	timeout := 35 * time.Second
	if val := os.Getenv("DAVE_READY_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		} else if sec, err := strconv.Atoi(val); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return timeout
}

func awaitVoiceReady(ctx context.Context, conn *DAVEVoiceConn) error {
	readyTimer := time.NewTimer(15 * time.Second)
	defer readyTimer.Stop()

	select {
	case <-conn.readyChan:
		conn.mu.Lock()
		conn.isEstablished = true
		ds := conn.daveSess
		conn.mu.Unlock()

		if ds != nil && ds.State().ProtocolVersion > 0 {
			timeout := daveReadyTimeout()
			waitCtx, cancelWait := context.WithTimeout(ctx, timeout)
			defer cancelWait()
			go func() {
				select {
				case <-conn.closeChan:
					cancelWait()
				case <-waitCtx.Done():
				}
			}()
			waitStart := time.Now()
			if _, err := ds.WaitReady(waitCtx); err != nil {
				if conn.IsClosed() {
					return fmt.Errorf("connection closed during DAVE E2EE negotiation")
				}
				return fmt.Errorf("timed out waiting for DAVE E2EE ready: %w", err)
			}
			slog.Info("DAVE E2EE ready wait finished", "guild_id", conn.guildID, "elapsed", time.Since(waitStart))
		}
		return nil
	case <-readyTimer.C:
		return fmt.Errorf("timed out waiting for DAVE Voice connection ready")
	case <-ctx.Done():
		return ctx.Err()
	case <-conn.closeChan:
		return fmt.Errorf("connection closed during establishment")
	}
}

func (c *DAVEVoiceConn) handleVoiceServerUpdate(update *discordgo.VoiceServerUpdate, setup chan<- *discordgo.VoiceServerUpdate) {
	if c == nil || update == nil || update.GuildID != c.guildID {
		return
	}
	c.mu.RLock()
	established := c.isEstablished
	closed := c.isClosed
	endpoint := c.endpoint
	c.mu.RUnlock()
	if established && !closed && update.Endpoint != "" && update.Endpoint != endpoint {
		c.triggerVoiceLoss()
		return
	}
	if setup != nil {
		select {
		case setup <- update:
		default:
		}
	}
}

func (c *DAVEVoiceConn) handleVoiceStateUpdate(update *discordgo.VoiceStateUpdate, setup chan<- *discordgo.VoiceStateUpdate) {
	if c == nil || update == nil || update.VoiceState == nil || update.GuildID != c.guildID {
		return
	}
	c.mu.RLock()
	channelID := c.channelID
	userID := c.userID
	c.mu.RUnlock()
	if update.ChannelID == channelID {
		if update.UserID != userID {
			c.mu.Lock()
			c.trackRemoteUserLocked(update.UserID, true)
			c.mu.Unlock()
		}
		c.withDave(func(ds *davesession.Session) { ds.AddUser(godave.UserID(update.UserID)) })
	} else if update.UserID != userID {
		c.mu.Lock()
		c.trackRemoteUserLocked(update.UserID, false)
		c.mu.Unlock()
		c.withDave(func(ds *davesession.Session) { ds.RemoveUser(godave.UserID(update.UserID)) })
	}
	if update.UserID == userID && setup != nil {
		select {
		case setup <- update:
		default:
		}
	}
}

func (c *DAVEVoiceConn) withDave(fn func(*davesession.Session)) {
	if c == nil || fn == nil {
		return
	}
	c.daveMu.Lock()
	defer c.daveMu.Unlock()
	c.mu.RLock()
	ds := c.daveSess
	closed := c.isClosed
	c.mu.RUnlock()
	if ds != nil && !closed {
		fn(ds)
	}
}

func (c *DAVEVoiceConn) sendWSOpcode(op int, data any) error {
	if c == nil {
		return fmt.Errorf("voice connection is nil")
	}

	payload := map[string]any{
		"op": op,
		"d":  data,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	c.wsWriteMu.Lock()
	defer c.wsWriteMu.Unlock()

	c.mu.RLock()
	ws := c.wsConn
	isClosed := c.isClosed
	c.mu.RUnlock()

	if isClosed || ws == nil {
		return fmt.Errorf("voice websocket closed")
	}

	_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return ws.WriteMessage(websocket.TextMessage, bytes)
}

func (c *DAVEVoiceConn) writeBinaryMessage(data []byte) error {
	if c == nil {
		return fmt.Errorf("voice connection is nil")
	}

	c.wsWriteMu.Lock()
	defer c.wsWriteMu.Unlock()

	c.mu.RLock()
	ws := c.wsConn
	isClosed := c.isClosed
	c.mu.RUnlock()

	if isClosed || ws == nil {
		return fmt.Errorf("voice websocket closed")
	}

	_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return ws.WriteMessage(websocket.BinaryMessage, data)
}

func (c *DAVEVoiceConn) sendHeartbeat() error {
	if c == nil {
		return fmt.Errorf("voice connection is nil")
	}
	c.mu.Lock()
	seq := c.lastSeq
	c.mu.Unlock()

	nonce := time.Now().UnixMilli()
	c.heartbeatsMu.Lock()
	c.outstandingHeartbeats[nonce] = time.Now()
	c.heartbeatsMu.Unlock()

	data := map[string]any{
		"t": nonce,
	}
	if seq >= 0 {
		data["seq_ack"] = seq
	}

	return c.sendWSOpcode(3, data)
}

func (c *DAVEVoiceConn) heartbeatLoop(interval time.Duration) {
	c.mu.Lock()
	c.heartbeatInterval = interval
	c.mu.Unlock()

	initTimer := time.NewTimer(1 * time.Second)
	select {
	case <-c.closeChan:
		initTimer.Stop()
		return
	case <-initTimer.C:
	}

	if c.IsClosed() {
		return
	}

	if err := c.sendHeartbeat(); err != nil {
		if !c.IsClosed() {
			slog.Warn("voice websocket heartbeat (Opcode 3) failed", "error", err)
			c.triggerVoiceLoss()
		}
		return
	}

	safeInterval := interval - (2 * time.Second)
	if safeInterval < 5*time.Second {
		safeInterval = interval
	}

	ticker := time.NewTicker(safeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.closeChan:
			return
		case <-ticker.C:
			if c.IsClosed() {
				return
			}
			c.heartbeatsMu.Lock()
			missed := len(c.outstandingHeartbeats)
			c.heartbeatsMu.Unlock()
			if missed >= 2 {
				if !c.IsClosed() {
					slog.Warn("voice websocket missed heartbeat ACKs", "guild_id", c.guildID, "unacked", missed)
					c.triggerVoiceLoss()
				}
				return
			}
			if err := c.sendHeartbeat(); err != nil {
				if !c.IsClosed() {
					slog.Warn("voice websocket heartbeat (Opcode 3) failed", "error", err)
					c.triggerVoiceLoss()
				}
				return
			}
		}
	}
}

func (c *DAVEVoiceConn) readLoop() {
	for {
		c.mu.RLock()
		ws := c.wsConn
		isClosed := c.isClosed
		interval := c.heartbeatInterval
		c.mu.RUnlock()

		if isClosed || ws == nil {
			return
		}
		if interval > 0 {
			_ = ws.SetReadDeadline(time.Now().Add(interval*2 + 5*time.Second))
		}

		msgType, data, err := ws.ReadMessage()
		if err != nil {
			c.mu.RLock()
			closed := c.isClosed
			c.mu.RUnlock()
			if !closed {
				slog.Warn("voice websocket disconnected", "guild_id", c.guildID, "error", err)
				c.triggerVoiceLoss()
			}
			return
		}
		if interval > 0 {
			_ = ws.SetReadDeadline(time.Now().Add(interval*2 + 5*time.Second))
		}

		if msgType == websocket.BinaryMessage {
			c.handleBinaryOpcode(data)
			continue
		}

		var packet struct {
			Op   int             `json:"op"`
			Seq  *int64          `json:"seq"`
			Data json.RawMessage `json:"d"`
		}
		if err := json.Unmarshal(data, &packet); err != nil {
			continue
		}

		if packet.Seq != nil {
			c.mu.Lock()
			c.lastSeq = *packet.Seq
			c.mu.Unlock()
		}

		c.handleOpcode(packet.Op, packet.Data)
	}
}

func (c *DAVEVoiceConn) handleBinaryOpcode(data []byte) {
	if len(data) < 3 {
		return
	}
	seq := binary.BigEndian.Uint16(data[0:2])
	c.mu.Lock()
	c.lastSeq = int64(seq)
	c.mu.Unlock()

	opcode := data[2]
	payload := data[3:]

	switch opcode {
	case 25:
		c.mu.RLock()
		alone := c.isAloneLocked()
		c.mu.RUnlock()
		slog.Info("DAVE received external sender package (opcode 25)", "guild_id", c.guildID, "bytes", len(payload), "alone", alone)
		c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSExternalSenderPackage(payload) })
		if alone {
			slog.Info("DAVE sole-member channel detected, activating sole-member transition (0, 1)", "guild_id", c.guildID)
			c.withDave(func(ds *davesession.Session) {
				ds.OnDavePrepareTransition(0, 1)
			})
		}
	case 27:
		slog.Info("DAVE received MLS proposals (opcode 27)", "guild_id", c.guildID, "bytes", len(payload))
		c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSProposals(payload) })
	case 29:
		if len(payload) >= 2 {
			tID := binary.BigEndian.Uint16(payload[0:2])
			slog.Info("DAVE received MLS prepare commit transition (opcode 29)", "guild_id", c.guildID, "transition_id", tID, "bytes", len(payload)-2)
			c.mu.Lock()
			c.serverTransitionID = tID
			c.mu.Unlock()
			c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSPrepareCommitTransition(tID, payload[2:]) })
		}
	case 30:
		if len(payload) >= 2 {
			tID := binary.BigEndian.Uint16(payload[0:2])
			slog.Info("DAVE received MLS welcome (opcode 30)", "guild_id", c.guildID, "transition_id", tID, "bytes", len(payload)-2)
			c.mu.Lock()
			c.serverTransitionID = tID
			c.mu.Unlock()
			c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSWelcome(tID, payload[2:]) })
		}
	}
}

func (c *DAVEVoiceConn) handleOpcode(op int, data json.RawMessage) {
	switch op {
	case 2:
		c.handleReadyOpcode(data)
	case 4:
		c.handleSessionDescriptionOpcode(data)
	case 6:
		var ack struct {
			Timestamp int64 `json:"t"`
		}
		if err := json.Unmarshal(data, &ack); err == nil && ack.Timestamp > 0 {
			c.heartbeatsMu.Lock()
			if _, ok := c.outstandingHeartbeats[ack.Timestamp]; ok {
				for t := range c.outstandingHeartbeats {
					if t <= ack.Timestamp {
						delete(c.outstandingHeartbeats, t)
					}
				}
				c.mu.RLock()
				ws := c.wsConn
				interval := c.heartbeatInterval
				c.mu.RUnlock()
				if ws != nil && interval > 0 {
					_ = ws.SetReadDeadline(time.Now().Add(interval*2 + 5*time.Second))
				}
			}
			c.heartbeatsMu.Unlock()
		}
	case 8:
		c.handleHelloOpcode(data)
	case 11, 18, 20:
		c.handleUserMembershipOpcode(op, data)
	case 12, 13:
		c.handleUserDisconnectOpcode(data)
	case 21, 22, 24, 29, 30:
		c.handleDaveTransitionOpcode(op, data)
	}
}

func (c *DAVEVoiceConn) handleReadyOpcode(data json.RawMessage) {
	var readyData struct {
		SSRC  uint32   `json:"ssrc"`
		IP    string   `json:"ip"`
		Port  int      `json:"port"`
		Modes []string `json:"modes"`
	}
	if err := json.Unmarshal(data, &readyData); err == nil {
		slog.Info("DAVE voice gateway ready (opcode 2)", "guild_id", c.guildID, "ssrc", readyData.SSRC, "ip", readyData.IP, "port", readyData.Port, "modes", readyData.Modes)
		c.mu.Lock()
		c.ssrc = readyData.SSRC
		c.mu.Unlock()

		c.withDave(func(ds *davesession.Session) { ds.AssignSsrcToCodec(readyData.SSRC, godave.CodecOpus) })
		c.setupUDP(readyData.IP, readyData.Port, readyData.Modes)
	}
}

func (c *DAVEVoiceConn) handleSessionDescriptionOpcode(data json.RawMessage) {
	var sessDesc struct {
		Mode                string `json:"mode"`
		SecretKey           []int  `json:"secret_key"`
		DaveProtocolVersion uint16 `json:"dave_protocol_version"`
	}
	if err := json.Unmarshal(data, &sessDesc); err == nil {
		slog.Info("DAVE session description received (opcode 4)", "guild_id", c.guildID, "mode", sessDesc.Mode, "dave_protocol_version", sessDesc.DaveProtocolVersion)
		if len(sessDesc.SecretKey) < 32 {
			c.Close()
			return
		}
		c.mu.Lock()
		c.selectedMode = sessDesc.Mode
		for i := 0; i < 32; i++ {
			c.secretKey[i] = byte(sessDesc.SecretKey[i])
		}
		for i := range sessDesc.SecretKey {
			sessDesc.SecretKey[i] = 0
		}
		c.mu.Unlock()

		if sessDesc.DaveProtocolVersion > 0 {
			c.withDave(func(ds *davesession.Session) { ds.OnSelectProtocolAck(sessDesc.DaveProtocolVersion) })
		}

		_ = c.resendSpeaking()

		c.mu.Lock()
		if !c.isClosed {
			if c.readyChan == nil {
				c.readyChan = make(chan struct{})
			}
			select {
			case <-c.readyChan:
			default:
				close(c.readyChan)
			}
		}
		c.mu.Unlock()
	}
}

func (c *DAVEVoiceConn) handleHelloOpcode(data json.RawMessage) {
	var helloData struct {
		HeartbeatInterval float64 `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(data, &helloData); err == nil && helloData.HeartbeatInterval > 0 {
		c.startWorker(func() {
			c.heartbeatLoop(time.Duration(helloData.HeartbeatInterval) * time.Millisecond)
		})
	}
}

func (c *DAVEVoiceConn) handleUserMembershipOpcode(op int, data json.RawMessage) {
	if op == 11 {
		var msg struct {
			UserIDs []string `json:"user_ids"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			c.mu.Lock()
			for _, uid := range msg.UserIDs {
				if uid != c.userID {
					c.trackRemoteUserLocked(uid, true)
				}
			}
			c.mu.Unlock()
			for _, uid := range msg.UserIDs {
				c.withDave(func(ds *davesession.Session) { ds.AddUser(godave.UserID(uid)) })
			}
		}
		return
	}

	var msg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &msg); err == nil && msg.UserID != "" {
		if msg.UserID != c.userID {
			c.mu.Lock()
			c.trackRemoteUserLocked(msg.UserID, true)
			c.mu.Unlock()
		}
		c.withDave(func(ds *davesession.Session) { ds.AddUser(godave.UserID(msg.UserID)) })
	}
}

func (c *DAVEVoiceConn) handleUserDisconnectOpcode(data json.RawMessage) {
	var msg struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &msg); err == nil && msg.UserID != "" {
		c.mu.Lock()
		c.trackRemoteUserLocked(msg.UserID, false)
		c.mu.Unlock()
		c.withDave(func(ds *davesession.Session) { ds.RemoveUser(godave.UserID(msg.UserID)) })
	}
}

func (c *DAVEVoiceConn) handleDaveTransitionOpcode(op int, data json.RawMessage) {
	switch op {
	case 21:
		var msg struct {
			TransitionID    uint16 `json:"transition_id"`
			ProtocolVersion uint16 `json:"protocol_version"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			slog.Info("DAVE prepare transition (opcode 21)", "guild_id", c.guildID, "transition_id", msg.TransitionID, "protocol_version", msg.ProtocolVersion)
			c.mu.Lock()
			c.serverTransitionID = msg.TransitionID
			c.mu.Unlock()
			c.withDave(func(ds *davesession.Session) { ds.OnDavePrepareTransition(msg.TransitionID, msg.ProtocolVersion) })
		}
	case 22:
		var msg struct {
			TransitionID uint16 `json:"transition_id"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			slog.Info("DAVE execute transition (opcode 22)", "guild_id", c.guildID, "transition_id", msg.TransitionID)
			c.withDave(func(ds *davesession.Session) {
				ds.OnDaveExecuteTransition(msg.TransitionID)
			})
			_ = c.resendSpeaking()
		}
	case 24:
		var msg struct {
			Epoch           int    `json:"epoch"`
			ProtocolVersion uint16 `json:"protocol_version"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			slog.Info("DAVE prepare epoch (opcode 24)", "guild_id", c.guildID, "epoch", msg.Epoch, "protocol_version", msg.ProtocolVersion)
			c.withDave(func(ds *davesession.Session) { ds.OnDavePrepareEpoch(msg.Epoch, msg.ProtocolVersion) })
		}
	case 29:
		var msg struct {
			TransitionID uint16 `json:"transition_id"`
			Commit       []byte `json:"commit"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			slog.Info("DAVE prepare commit transition (opcode 29)", "guild_id", c.guildID, "transition_id", msg.TransitionID, "commit_bytes", len(msg.Commit))
			c.mu.Lock()
			c.serverTransitionID = msg.TransitionID
			c.mu.Unlock()
			c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSPrepareCommitTransition(msg.TransitionID, msg.Commit) })
		}
	case 30:
		var msg struct {
			TransitionID uint16 `json:"transition_id"`
			Welcome      []byte `json:"welcome"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			slog.Info("DAVE MLS welcome (opcode 30)", "guild_id", c.guildID, "transition_id", msg.TransitionID, "welcome_bytes", len(msg.Welcome))
			c.mu.Lock()
			c.serverTransitionID = msg.TransitionID
			c.mu.Unlock()
			c.withDave(func(ds *davesession.Session) { ds.OnDaveMLSWelcome(msg.TransitionID, msg.Welcome) })
		}
	}
}

func selectEncryptionMode(modes []string) (string, error) {
	preferred := []string{
		"aead_aes256_gcm_rtpsize",
		"aead_xchacha20_poly1305_rtpsize",
		"xsalsa20_poly1305_suffix",
		"xsalsa20_poly1305",
	}
	for _, pref := range preferred {
		for _, m := range modes {
			if m == pref {
				return m, nil
			}
		}
	}
	return "", fmt.Errorf("no supported encryption mode offered by voice gateway: %v", modes)
}

func (c *DAVEVoiceConn) setupUDP(ip string, port int, modes []string) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return
	}

	udpConn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return
	}
	_ = udpConn.SetWriteBuffer(512 * 1024)
	_ = udpConn.SetReadBuffer(512 * 1024)

	c.mu.Lock()
	if c.isClosed {
		_ = udpConn.Close()
		c.mu.Unlock()
		return
	}
	c.targetUDPAddr = addr
	c.udpConn = udpConn
	ssrc := c.ssrc
	c.mu.Unlock()

	packet := make([]byte, 74)
	binary.BigEndian.PutUint16(packet[0:2], 1)
	binary.BigEndian.PutUint16(packet[2:4], 70)
	binary.BigEndian.PutUint32(packet[4:8], ssrc)

	if _, err := udpConn.Write(packet); err != nil {
		c.clearUDP(udpConn)
		return
	}

	response := make([]byte, 74)
	_ = udpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := udpConn.ReadFromUDP(response)
	_ = udpConn.SetReadDeadline(time.Time{})

	if err != nil || n < 74 {
		c.clearUDP(udpConn)
		return
	}

	myIP := strings.TrimRight(string(response[8:72]), "\x00")
	myPort := binary.BigEndian.Uint16(response[72:74])

	chosenMode, errMode := selectEncryptionMode(modes)
	if errMode != nil {
		c.Close()
		return
	}

	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		c.clearUDP(udpConn)
		return
	}
	c.selectedMode = chosenMode
	c.mu.Unlock()

	if err := c.sendWSOpcode(1, map[string]any{
		"protocol": "udp",
		"data": map[string]any{
			"address": myIP,
			"port":    myPort,
			"mode":    chosenMode,
		},
	}); err != nil {
		c.clearUDP(udpConn)
	}
}

func (c *DAVEVoiceConn) clearUDP(udpConn *net.UDPConn) {
	if udpConn == nil {
		return
	}
	c.mu.Lock()
	if c.udpConn == udpConn {
		c.udpConn = nil
	}
	c.mu.Unlock()
	_ = udpConn.Close()
}

func (c *DAVEVoiceConn) SetSpeaking(speaking bool) error {
	if c == nil {
		return fmt.Errorf("voice connection closed")
	}
	c.speakingMu.Lock()
	defer c.speakingMu.Unlock()
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return fmt.Errorf("voice connection closed")
	}
	c.speaking = speaking
	c.speakingSet = true
	ssrc := c.ssrc
	c.mu.Unlock()
	return c.sendSpeaking(speaking, ssrc)
}

func (c *DAVEVoiceConn) resendSpeaking() error {
	if c == nil {
		return fmt.Errorf("voice connection closed")
	}
	c.speakingMu.Lock()
	defer c.speakingMu.Unlock()
	c.mu.RLock()
	if c.isClosed {
		c.mu.RUnlock()
		return fmt.Errorf("voice connection closed")
	}
	speakingSet := c.speakingSet
	speaking := c.speaking
	ssrc := c.ssrc
	c.mu.RUnlock()
	if !speakingSet {
		return nil
	}
	return c.sendSpeaking(speaking, ssrc)
}

func (c *DAVEVoiceConn) sendSpeaking(speaking bool, ssrc uint32) error {
	val := 0
	if speaking {
		val = 1
	}
	return c.sendWSOpcode(5, map[string]any{
		"speaking": val,
		"delay":    0,
		"ssrc":     ssrc,
	})
}

func (c *DAVEVoiceConn) SendOpus(opusFrame []byte) error {
	if c == nil {
		return fmt.Errorf("voice connection closed")
	}
	c.mu.Lock()
	if c.isClosed || c.udpConn == nil {
		c.mu.Unlock()
		return fmt.Errorf("voice connection closed")
	}

	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80
	rtpHeader[1] = 0x78
	binary.BigEndian.PutUint16(rtpHeader[2:4], c.sequence)
	binary.BigEndian.PutUint32(rtpHeader[4:8], c.timestamp)
	binary.BigEndian.PutUint32(rtpHeader[8:12], c.ssrc)

	c.sequence++
	c.timestamp += 960
	udpConn := c.udpConn
	ssrc := c.ssrc
	c.mu.Unlock()

	payload, encrypted, errDave := c.encryptDave(ssrc, opusFrame)
	if errDave != nil {
		return errDave
	}
	if !encrypted {
		c.mu.RLock()
		ds := c.daveSess
		c.mu.RUnlock()
		if ds != nil && ds.State().ProtocolVersion > 0 {
			return ErrDAVEFrameHeld
		}
		payload = opusFrame
	}

	fullPacket, errEncrypt := c.encryptRTP(rtpHeader, payload)
	if errEncrypt != nil {
		return errEncrypt
	}

	_, err := udpConn.Write(fullPacket)
	return err
}

func (c *DAVEVoiceConn) encryptDave(ssrc uint32, frame []byte) ([]byte, bool, error) {
	c.daveMu.Lock()
	defer c.daveMu.Unlock()
	c.mu.RLock()
	ds := c.daveSess
	closed := c.isClosed
	c.mu.RUnlock()
	if ds == nil || closed {
		return nil, false, nil
	}

	state := ds.State()
	if state.ProtocolVersion > 0 {
		if ds.ShouldHoldFrames() || !ds.Ready() {
			return nil, false, ErrDAVEFrameHeld
		}
	} else if !ds.Ready() {
		return nil, false, nil
	}

	buffer := make([]byte, ds.MaxEncryptedFrameSize(len(frame)))
	n, err := ds.Encrypt(ssrc, frame, buffer)
	if err != nil {
		return nil, true, fmt.Errorf("dave encryption failed: %w", err)
	}
	return buffer[:n], true, nil
}

func (c *DAVEVoiceConn) encryptRTP(header, payload []byte) ([]byte, error) {
	c.mu.Lock()
	if c.nonceCounter == ^uint32(0) {
		c.mu.Unlock()
		return nil, fmt.Errorf("voice nonce counter exhausted")
	}
	mode := c.selectedMode
	key := c.secretKey
	counter := c.nonceCounter
	c.nonceCounter++
	c.mu.Unlock()

	switch mode {
	case "aead_aes256_gcm_rtpsize":
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		nonceBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(nonceBuf, counter)

		nonce := make([]byte, 12)
		copy(nonce[0:4], nonceBuf)

		encrypted := gcm.Seal(nil, nonce, payload, header)
		packet := make([]byte, 0, len(header)+len(encrypted)+4)
		packet = append(packet, header...)
		packet = append(packet, encrypted...)
		packet = append(packet, nonceBuf...)
		return packet, nil

	case "aead_xchacha20_poly1305_rtpsize":
		aead, err := chacha20poly1305.NewX(key[:])
		if err != nil {
			return nil, err
		}
		nonceBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(nonceBuf, counter)

		nonce := make([]byte, 24)
		copy(nonce[0:4], nonceBuf)

		encrypted := aead.Seal(nil, nonce, payload, header)
		packet := make([]byte, 0, len(header)+len(encrypted)+4)
		packet = append(packet, header...)
		packet = append(packet, encrypted...)
		packet = append(packet, nonceBuf...)
		return packet, nil

	case "xsalsa20_poly1305_suffix":
		var nonce [24]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		encrypted := secretbox.Seal(nil, payload, &nonce, &key)
		packet := make([]byte, 0, len(header)+len(encrypted)+24)
		packet = append(packet, header...)
		packet = append(packet, encrypted...)
		packet = append(packet, nonce[:]...)
		return packet, nil

	case "xsalsa20_poly1305":
		var nonce [24]byte
		copy(nonce[:], header)
		encrypted := secretbox.Seal(nil, payload, &nonce, &key)
		packet := make([]byte, 0, len(header)+len(encrypted))
		packet = append(packet, header...)
		packet = append(packet, encrypted...)
		return packet, nil

	default:
		return nil, fmt.Errorf("unsupported encryption mode: %s", mode)
	}
}

func (c *DAVEVoiceConn) Close() {
	if c == nil {
		return
	}
	c.workerMu.Lock()
	c.mu.Lock()
	if c.closeChan == nil {
		c.closeChan = make(chan struct{})
	}
	if c.isClosed {
		c.mu.Unlock()
		c.workerMu.Unlock()
		return
	}
	c.isClosed = true

	for i := range c.secretKey {
		c.secretKey[i] = 0
	}

	close(c.closeChan)

	handlers := c.removeHandlers
	c.removeHandlers = nil
	ds := c.daveSess
	c.daveSess = nil
	wsConn := c.wsConn
	c.wsConn = nil
	udpConn := c.udpConn
	c.udpConn = nil
	c.mu.Unlock()
	c.workerMu.Unlock()

	for _, remove := range handlers {
		if remove != nil {
			remove()
		}
	}

	if ds != nil {
		c.daveMu.Lock()
		_ = ds.Close()
		c.daveMu.Unlock()
	}
	if wsConn != nil {
		c.wsWriteMu.Lock()
		_ = wsConn.Close()
		c.wsWriteMu.Unlock()
	}
	if udpConn != nil {
		_ = udpConn.Close()
	}
}

func (c *DAVEVoiceConn) IsClosed() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isClosed
}
