package auth

import (
	"context"
	"fmt"
	"math"

	"github.com/bwmarrin/discordgo"
)

type Authorizer struct {
	state    StateProvider
	ownerIDs map[string]struct{}
}

func NewAuthorizer(state StateProvider, ownerIDs []string) *Authorizer {
	owners := make(map[string]struct{}, len(ownerIDs))
	for _, id := range ownerIDs {
		if id != "" {
			owners[id] = struct{}{}
		}
	}
	return &Authorizer{
		state:    state,
		ownerIDs: owners,
	}
}

func (a *Authorizer) isBotOwner(userID string) bool {
	if userID == "" {
		return false
	}
	_, ok := a.ownerIDs[userID]
	return ok
}

func (a *Authorizer) Evaluate(ctx context.Context, req Request) Decision {
	if req.GuildID == "" {
		return Fail(ErrGuildRequired, "Guild context is required.")
	}

	actorID := req.ActorID
	if actorID == "" && req.ActorMember != nil {
		if req.ActorMember.User != nil {
			actorID = req.ActorMember.User.ID
		} else {
			actorID = "unknown_actor"
		}
	}
	if actorID == "" {
		return Fail(ErrActorRequired, "Mod ID is required.")
	}
	req.ActorID = actorID

	guild, err := a.state.GetGuild(ctx, req.GuildID)
	if err != nil || guild == nil {
		return Fail(err, "Unable to verify server ownership.")
	}
	ownerID := guild.OwnerID

	targetMember := req.TargetMember
	targetUserID := req.TargetUserID
	if targetMember != nil && targetMember.User != nil {
		targetUserID = targetMember.User.ID
	} else if targetUserID != "" && targetMember == nil {
		if member, errMem := a.state.GetMember(ctx, req.GuildID, targetUserID); errMem == nil && member != nil {
			targetMember = member
		}
	}

	var rolesMap map[string]*discordgo.Role
	getRolesMap := func() (map[string]*discordgo.Role, error) {
		if rolesMap != nil {
			return rolesMap, nil
		}
		rolesList, errRoles := a.state.GetRoles(ctx, req.GuildID)
		if errRoles != nil {
			return nil, errRoles
		}
		rolesMap = make(map[string]*discordgo.Role, len(rolesList))
		for _, r := range rolesList {
			if r != nil {
				rolesMap[r.ID] = r
			}
		}
		return rolesMap, nil
	}

	targetRole := req.TargetRole
	if targetRole == nil && req.TargetRoleID != "" {
		rmap, errR := getRolesMap()
		if errR != nil {
			return Fail(errR, "Unable to fetch server roles.")
		}
		r, exists := rmap[req.TargetRoleID]
		if !exists {
			return Fail(ErrRoleNotFound, "Target role was not found.")
		}
		targetRole = r
	}

	if targetUserID != "" && targetUserID == ownerID && ownerID != "" {
		return Deny("You cannot moderate the server owner.")
	}

	if targetUserID != "" && targetUserID == req.ActorID {
		return Deny("You cannot perform this action on yourself.")
	}

	actorIsBotOwner := a.isBotOwner(req.ActorID)
	actorIsGuildOwner := req.ActorID == ownerID && ownerID != ""

	if !actorIsBotOwner && !actorIsGuildOwner {
		if req.RequiredPerm != 0 {
			perms, errPerm := a.state.ComputePermissions(ctx, req.GuildID, req.ChannelID, req.ActorID)
			if errPerm != nil {
				return Fail(errPerm, "Failed to verify channel permissions.")
			}
			if perms&discordgo.PermissionAdministrator == 0 && perms&req.RequiredPerm == 0 {
				return Deny("You do not have permission to execute this command.")
			}
		}

		if targetMember != nil || targetRole != nil {
			rmap, errR := getRolesMap()
			if errR != nil {
				return Fail(errR, "Unable to fetch server roles.")
			}
			actorMember := req.ActorMember
			if actorMember == nil {
				am, errActor := a.state.GetMember(ctx, req.GuildID, req.ActorID)
				if errActor != nil || am == nil {
					return Fail(errActor, "Failed to verify actor member status.")
				}
				actorMember = am
			}
			actorPos := MemberHighestPosition(ownerID, actorMember, rmap)

			if targetMember != nil {
				targetPos := MemberHighestPosition(ownerID, targetMember, rmap)
				if actorPos <= targetPos {
					return Deny("You cannot moderate a member with an equal or higher role.")
				}
			}

			if targetRole != nil {
				if actorPos <= targetRole.Position {
					return Deny("You cannot modify or assign a role with an equal or higher position than your highest role.")
				}
			}
		}
	}

	if req.CheckBot {
		botID := a.state.BotUserID()
		if botID == "" {
			return Fail(ErrBotIDUnavailable, "Unable to verify bot identity.")
		}

		if req.RequiredPerm != 0 {
			botPerms, errBotPerm := a.state.ComputePermissions(ctx, req.GuildID, req.ChannelID, botID)
			if errBotPerm != nil {
				return Fail(errBotPerm, "Failed to verify bot channel permissions.")
			}
			if botPerms&discordgo.PermissionAdministrator == 0 && botPerms&req.RequiredPerm == 0 {
				return Deny("The bot lacks permission to execute this action.")
			}
		}

		if targetRole != nil && targetRole.Managed {
			return Deny("This role is managed by an external integration and cannot be modified directly.")
		}

		if targetMember != nil || targetRole != nil {
			rmap, errR := getRolesMap()
			if errR != nil {
				return Fail(errR, "Unable to fetch server roles.")
			}
			botMember, errBot := a.state.GetMember(ctx, req.GuildID, botID)
			if errBot != nil || botMember == nil {
				return Fail(errBot, "Failed to fetch bot member status.")
			}
			botPos := MemberHighestPosition(ownerID, botMember, rmap)

			if targetMember != nil {
				targetPos := MemberHighestPosition(ownerID, targetMember, rmap)
				if botPos <= targetPos {
					return Deny("The bot's highest role is not positioned high enough to moderate this member.")
				}
			}

			if targetRole != nil {
				if botPos <= targetRole.Position {
					return Deny(fmt.Sprintf("The bot's highest role is positioned below or equal to role **%s**.", targetRole.Name))
				}
			}
		}
	}

	return Allow()
}

// HighestRolePosition returns the highest role hierarchy position.
// (math.MaxInt32 = guild owner)
func HighestRolePosition(ownerID, userID string, roleIDs []string, getPos func(string) (int, bool)) int {
	if userID == "" {
		return 0
	}
	if ownerID != "" && userID == ownerID {
		return math.MaxInt32
	}
	highest := 0
	for _, rID := range roleIDs {
		if pos, ok := getPos(rID); ok {
			if pos > highest {
				highest = pos
			}
		}
	}
	return highest
}

func MemberHighestPosition(ownerID string, member *discordgo.Member, rolesMap map[string]*discordgo.Role) int {
	if member == nil {
		return 0
	}
	userID := ""
	if member.User != nil {
		userID = member.User.ID
	}
	return HighestRolePosition(ownerID, userID, member.Roles, func(id string) (int, bool) {
		if r, ok := rolesMap[id]; ok && r != nil {
			return r.Position, true
		}
		return 0, false
	})
}
