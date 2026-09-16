package roles

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/database"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type defaultRoleManager struct {
	client RoleClient
	store  Store
}

func NewRoleManager(client RoleClient, store Store) RoleManager {
	return &defaultRoleManager{
		client: client,
		store:  store,
	}
}

func DefaultRoleManager(s *discordgo.Session, db *database.DB) RoleManager {
	return NewRoleManager(NewDiscordRoleAdapter(s), db)
}

func (m *defaultRoleManager) enforceAuthority(ctx context.Context, guildID, actorUserID, targetUserID, targetRoleID string) error {
	ownerID, errOwner := m.client.FetchOwnerID(ctx, guildID)
	if errOwner != nil {
		return fmt.Errorf("failed to fetch guild owner: %w", errOwner)
	}

	botID := m.client.BotUserID()
	if botID == "" {
		return fmt.Errorf("unable to determine bot identity")
	}

	botMember, errBot := m.client.FetchMember(ctx, guildID, botID)
	if errBot != nil || botMember.UserID == "" {
		return fmt.Errorf("failed to fetch bot member: %w", errBot)
	}

	rolesMap, errRoles := m.client.FetchRoles(ctx, guildID)
	if errRoles != nil {
		return fmt.Errorf("failed to fetch guild roles: %w", errRoles)
	}

	var actorMember MemberInfo
	if actorUserID != "" {
		am, errActor := m.client.FetchMember(ctx, guildID, actorUserID)
		if errActor != nil {
			return fmt.Errorf("failed to fetch actor member: %w", errActor)
		}
		actorMember = am
	}

	var targetMember MemberInfo
	if targetUserID != "" {
		tm, errTarget := m.client.FetchMember(ctx, guildID, targetUserID)
		if errTarget != nil {
			return fmt.Errorf("failed to fetch target member: %w", errTarget)
		}
		targetMember = tm
	}

	var targetRole *RoleInfo
	if targetRoleID != "" {
		r, exists := rolesMap[targetRoleID]
		if !exists {
			return ErrRoleNotFound
		}
		targetRole = &r
	}

	return EvaluateHierarchy(ownerID, botMember, actorMember, targetMember, targetRole, rolesMap)
}

func (m *defaultRoleManager) CheckAuthority(ctx context.Context, req AuthRequest) error {
	return m.enforceAuthority(ctx, req.GuildID, req.ActorUserID, req.TargetUserID, req.TargetRoleID)
}

func (m *defaultRoleManager) Assign(ctx context.Context, req AssignRequest) error {
	if req.ActorUserID != "" {
		if err := m.enforceAuthority(ctx, req.GuildID, req.ActorUserID, req.TargetUserID, req.RoleID); err != nil {
			return err
		}
	}

	if req.Duration != 0 {
		if req.Duration < 5*time.Second || req.Duration > 365*24*time.Hour {
			return ErrInvalidDuration
		}
	}

	if req.Overwrite != OverwriteNone {
		if errOv := m.client.EnsureChannelOverwrite(ctx, req.GuildID, req.RoleID, req.Overwrite, ""); errOv != nil {
			logger.Warnf("[ROLES] Failed to ensure channel overwrite %v for role %s: %v", req.Overwrite, req.RoleID, errOv)
		}
	}

	unlock := sharedRoleLocks.lock(req.GuildID, req.TargetUserID, req.RoleID)
	defer unlock()

	if err := m.client.AddRole(ctx, req.GuildID, req.TargetUserID, req.RoleID); err != nil {
		return fmt.Errorf("failed to add role: %w", err)
	}

	if req.Duration > 0 {
		expiresAt := time.Now().Add(req.Duration)
		if errTemp := m.store.SaveTempRole(req.GuildID, req.TargetUserID, req.RoleID, expiresAt, req.AssignedBy); errTemp != nil {
			if rbErr := m.client.RemoveRole(ctx, req.GuildID, req.TargetUserID, req.RoleID); rbErr != nil {
				return fmt.Errorf("failed to persist temporary role expiry: %w (compensation failed: %v)", errTemp, rbErr)
			}
			return fmt.Errorf("failed to persist temporary role expiry: %w (role grant rolled back)", errTemp)
		}
	}

	return nil
}

func (m *defaultRoleManager) Revoke(ctx context.Context, req RevokeRequest) error {
	if req.ActorUserID != "" {
		if err := m.enforceAuthority(ctx, req.GuildID, req.ActorUserID, req.TargetUserID, req.RoleID); err != nil {
			return err
		}
	}

	unlock := sharedRoleLocks.lock(req.GuildID, req.TargetUserID, req.RoleID)
	defer unlock()

	if errRem := m.client.RemoveRole(ctx, req.GuildID, req.TargetUserID, req.RoleID); errRem != nil {
		return fmt.Errorf("failed to remove role: %w", errRem)
	}

	if errDB := m.store.RemoveTempRole(req.GuildID, req.TargetUserID, req.RoleID); errDB != nil {
		logger.Warnf("[ROLES] Failed to remove temp role record for %s: %v", req.TargetUserID, errDB)
	}

	return nil
}

func (m *defaultRoleManager) Jail(ctx context.Context, req JailRequest) ([]string, error) {
	if req.ActorUserID != "" {
		if err := m.enforceAuthority(ctx, req.GuildID, req.ActorUserID, req.TargetUserID, req.JailRoleID); err != nil {
			return nil, err
		}
	}

	unlock := sharedRoleLocks.lock(req.GuildID, req.TargetUserID, req.JailRoleID)
	defer unlock()

	if errOv := m.client.EnsureChannelOverwrite(ctx, req.GuildID, req.JailRoleID, OverwriteJail, req.JailChannelID); errOv != nil {
		logger.Warnf("[JAIL] Failed to ensure jail channel overwrites for %s: %v", req.JailRoleID, errOv)
	}

	targetMember, errMem := m.client.FetchMember(ctx, req.GuildID, req.TargetUserID)
	if errMem != nil {
		return nil, fmt.Errorf("failed to fetch target member: %w", errMem)
	}

	allTemp, errTemp := m.store.GetUserTempRoles(req.GuildID, req.TargetUserID)
	if errTemp != nil {
		logger.Warnf("[JAIL] Failed to query user temp roles: %v", errTemp)
	}
	isTemp := make(map[string]bool)
	for _, tr := range allTemp {
		isTemp[tr.RoleID] = true
	}

	var rolesToStrip []string
	var savedRoles []string
	for _, rID := range targetMember.RoleIDs {
		if rID == req.GuildID || rID == req.JailRoleID {
			continue
		}
		rolesToStrip = append(rolesToStrip, rID)
		if !isTemp[rID] {
			savedRoles = append(savedRoles, rID)
		}
	}

	if errSave := m.store.SaveJailedUser(req.GuildID, req.TargetUserID, savedRoles, req.ActorUserID, req.Reason); errSave != nil {
		return nil, errSave
	}

	var stripped []string
	for _, rID := range rolesToStrip {
		if errStrip := m.client.RemoveRole(ctx, req.GuildID, req.TargetUserID, rID); errStrip != nil {
			var rbErrs []string
			for _, prev := range stripped {
				if rbErr := m.client.AddRole(ctx, req.GuildID, req.TargetUserID, prev); rbErr != nil {
					rbErrs = append(rbErrs, fmt.Sprintf("%s: %v", prev, rbErr))
				}
			}
			_ = m.store.RemoveJailedUser(req.GuildID, req.TargetUserID)
			if len(rbErrs) > 0 {
				return nil, fmt.Errorf("failed to strip role %s: %w (compensation failed: %s)", rID, errStrip, strings.Join(rbErrs, ", "))
			}
			return nil, fmt.Errorf("failed to strip role %s: %w (rolled back)", rID, errStrip)
		}
		stripped = append(stripped, rID)
	}

	if errJail := m.client.AddRole(ctx, req.GuildID, req.TargetUserID, req.JailRoleID); errJail != nil {
		var rbErrs []string
		for _, prev := range stripped {
			if rbErr := m.client.AddRole(ctx, req.GuildID, req.TargetUserID, prev); rbErr != nil {
				rbErrs = append(rbErrs, fmt.Sprintf("%s: %v", prev, rbErr))
			}
		}
		_ = m.store.RemoveJailedUser(req.GuildID, req.TargetUserID)
		if len(rbErrs) > 0 {
			return nil, fmt.Errorf("failed to assign jail role: %w (compensation failed: %s)", errJail, strings.Join(rbErrs, ", "))
		}
		return nil, fmt.Errorf("failed to assign jail role: %w (rolled back stripped roles)", errJail)
	}

	return stripped, nil
}

func (m *defaultRoleManager) Unjail(ctx context.Context, req UnjailRequest) (UnjailResult, error) {
	var res UnjailResult

	if req.ActorUserID != "" {
		if err := m.enforceAuthority(ctx, req.GuildID, req.ActorUserID, req.TargetUserID, req.JailRoleID); err != nil {
			return res, err
		}
	}

	unlock := sharedRoleLocks.lock(req.GuildID, req.TargetUserID, req.JailRoleID)
	defer unlock()

	jailed, errJ := m.store.GetJailedUserInfo(req.GuildID, req.TargetUserID)
	if errJ != nil || jailed == nil {
		return res, ErrNotJailed
	}

	rolesMap, errRoles := m.client.FetchRoles(ctx, req.GuildID)
	if errRoles != nil {
		return res, fmt.Errorf("failed to fetch guild roles: %w", errRoles)
	}

	ownerID, _ := m.client.FetchOwnerID(ctx, req.GuildID)
	botID := m.client.BotUserID()
	if botID == "" {
		return res, fmt.Errorf("unable to determine bot identity")
	}

	botMember, errBot := m.client.FetchMember(ctx, guildIDOrDefault(req.GuildID), botID)
	if errBot != nil || botMember.UserID == "" {
		return res, fmt.Errorf("failed to fetch bot member: %w", errBot)
	}
	botPos := memberHighestPosition(ownerID, botMember, rolesMap)

	actorPos := 0
	actorIsOwner := false
	if req.ActorUserID != "" {
		if am, errActor := m.client.FetchMember(ctx, req.GuildID, req.ActorUserID); errActor == nil {
			actorPos = memberHighestPosition(ownerID, am, rolesMap)
		}
		actorIsOwner = req.ActorUserID == ownerID
	}

	activeTemp, _ := m.store.GetActiveTempRolesForUser(req.GuildID, req.TargetUserID)
	activeTempMap := make(map[string]bool)
	for _, tr := range activeTemp {
		activeTempMap[tr.RoleID] = true
	}

	for _, rID := range jailed.Roles {
		role, exists := rolesMap[rID]
		if !exists {
			res.Missed = append(res.Missed, rID)
			continue
		}

		if !req.Force {
			if !actorIsOwner && req.ActorUserID != "" && actorPos <= role.Position {
				res.Missed = append(res.Missed, role.Name)
				continue
			}
			if botPos <= role.Position {
				res.Missed = append(res.Missed, role.Name)
				continue
			}
		}

		if errAdd := m.client.AddRole(ctx, req.GuildID, req.TargetUserID, rID); errAdd != nil {
			res.Missed = append(res.Missed, role.Name)
		} else {
			res.Restored = append(res.Restored, rID)
		}
	}

	for trRoleID := range activeTempMap {
		alreadyRestored := false
		for _, restID := range res.Restored {
			if restID == trRoleID {
				alreadyRestored = true
				break
			}
		}
		if !alreadyRestored {
			_ = m.client.AddRole(ctx, req.GuildID, req.TargetUserID, trRoleID)
		}
	}

	if len(res.Missed) > 0 && !req.Force {
		return res, nil
	}

	if req.JailRoleID != "" {
		if errRemJail := m.client.RemoveRole(ctx, req.GuildID, req.TargetUserID, req.JailRoleID); errRemJail != nil {
			res.Missed = append(res.Missed, "jail role")
			return res, fmt.Errorf("failed to remove jail role: %w", errRemJail)
		}
	}

	_ = m.store.RemoveJailedUser(req.GuildID, req.TargetUserID)
	return res, nil
}

func (m *defaultRoleManager) RestoreOnRejoin(ctx context.Context, req RejoinRequest) ([]string, error) {
	unlock := sharedRoleLocks.lock(req.GuildID, req.UserID, req.JailRoleID)
	defer unlock()

	if req.IsJailed {
		if req.JailRoleID != "" {
			_ = m.client.AddRole(ctx, req.GuildID, req.UserID, req.JailRoleID)
		}
		return nil, nil
	}

	var restored []string
	rolesMap, _ := m.client.FetchRoles(ctx, req.GuildID)

	for _, roleID := range req.AutoroleIDs {
		if r, ok := rolesMap[roleID]; ok && req.AdminPermFlags != 0 && (r.Permissions&req.AdminPermFlags) != 0 {
			continue
		}
		if err := m.client.AddRole(ctx, req.GuildID, req.UserID, roleID); err == nil {
			restored = append(restored, roleID)
		}
	}

	activeTemp, _ := m.store.GetActiveTempRolesForUser(req.GuildID, req.UserID)
	for _, tr := range activeTemp {
		if err := m.client.AddRole(ctx, req.GuildID, req.UserID, tr.RoleID); err == nil {
			restored = append(restored, tr.RoleID)
		}
	}

	for _, roleID := range req.SavedRoleIDs {
		if roleID == req.MuteRoleID || roleID == req.JailRoleID {
			continue
		}
		if r, ok := rolesMap[roleID]; ok && req.AdminPermFlags != 0 && (r.Permissions&req.AdminPermFlags) != 0 {
			continue
		}
		if err := m.client.AddRole(ctx, req.GuildID, req.UserID, roleID); err == nil {
			restored = append(restored, roleID)
		}
	}

	return restored, nil
}

func (m *defaultRoleManager) SweepExpired(ctx context.Context) (int, error) {
	expired, err := m.store.GetExpiredTempRoles()
	if err != nil {
		return 0, err
	}

	swept := 0
	for _, entry := range expired {
		select {
		case <-ctx.Done():
			return swept, ctx.Err()
		default:
		}

		if m.sweepSingleExpired(ctx, entry) {
			swept++
		}
	}

	return swept, nil
}

func (m *defaultRoleManager) sweepSingleExpired(ctx context.Context, entry database.TempRoleEntry) bool {
	unlock := sharedRoleLocks.lock(entry.GuildID, entry.UserID, entry.RoleID)
	defer unlock()

	claimed, errDel := m.store.DeleteExpiredTempRole(entry.GuildID, entry.UserID, entry.RoleID, entry.ExpiresAt)
	if errDel != nil {
		logger.Warnf("[TEMPROLES] Failed to claim expired role %s: %v", entry.RoleID, errDel)
		return false
	}
	if !claimed {
		return false
	}

	errRem := m.client.RemoveRole(ctx, entry.GuildID, entry.UserID, entry.RoleID)
	if errRem != nil {
		errStr := errRem.Error()
		if strings.Contains(errStr, "10007") || strings.Contains(errStr, "10011") || strings.Contains(errStr, "Unknown Member") || strings.Contains(errStr, "Unknown Role") {
		} else {
			logger.Warnf("[TEMPROLES] Failed to remove expired role %s from %s: %v", entry.RoleID, entry.UserID, errRem)
		}
	}
	return true
}

func guildIDOrDefault(gID string) string {
	return gID
}
