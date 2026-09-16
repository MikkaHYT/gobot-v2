package roles

import (
	"gobot/internal/policy/auth"
)

func memberHighestPosition(ownerID string, member MemberInfo, rolesMap map[string]RoleInfo) int {
	return auth.HighestRolePosition(ownerID, member.UserID, member.RoleIDs, func(roleID string) (int, bool) {
		if r, ok := rolesMap[roleID]; ok {
			return r.Position, true
		}
		return 0, false
	})
}

func EvaluateHierarchy(ownerID string, botMember, actorMember, targetMember MemberInfo, targetRole *RoleInfo, rolesMap map[string]RoleInfo) error {
	actorID := actorMember.UserID
	targetID := targetMember.UserID

	if actorID != "" && targetID != "" && actorID == targetID {
		return ErrSelfModeration
	}

	if targetID != "" && targetID == ownerID && ownerID != "" {
		return ErrOwnerModeration
	}

	actorPos := memberHighestPosition(ownerID, actorMember, rolesMap)

	if targetID != "" {
		targetPos := memberHighestPosition(ownerID, targetMember, rolesMap)
		if actorID != ownerID && actorPos <= targetPos {
			return ErrHierarchyActorTarget
		}
		if botMember.UserID != "" {
			botPos := memberHighestPosition(ownerID, botMember, rolesMap)
			if botPos <= targetPos {
				return ErrHierarchyBotTarget
			}
		}
	}

	if targetRole != nil {
		if actorID != ownerID && actorPos <= targetRole.Position {
			return ErrHierarchyActorRole
		}
		if botMember.UserID != "" {
			botPos := memberHighestPosition(ownerID, botMember, rolesMap)
			if botPos <= targetRole.Position {
				return ErrHierarchyBotRole
			}
		}
	}

	return nil
}
