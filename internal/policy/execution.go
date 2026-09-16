package policy

import "strings"

type ExecutionOutcome string

const (
	OutcomeAllow ExecutionOutcome = "allow"
	OutcomeDeny  ExecutionOutcome = "deny"
)

type ExecutionReason string

const (
	ReasonAllowed                        ExecutionReason = "allowed"
	ReasonUnavailable                    ExecutionReason = "policy_unavailable"
	ReasonGlobalGuildRestricted          ExecutionReason = "global_guild_restricted"
	ReasonGlobalUserRestricted           ExecutionReason = "global_user_restricted"
	ReasonConfiguredGlobalUserRestricted ExecutionReason = "configured_global_user_restricted"
	ReasonCanonicalCommandDisabled       ExecutionReason = "canonical_command_disabled"
	ReasonGuildChannelRestricted         ExecutionReason = "guild_channel_restricted"
	ReasonGuildUserRestricted            ExecutionReason = "guild_user_restricted"
	ReasonGuildRoleRestricted            ExecutionReason = "guild_role_restricted"
	ReasonInvokedCommandDisabled         ExecutionReason = "invoked_command_disabled"
)

type ExecutionRequest struct {
	GuildID       string
	ChannelID     string
	UserID        string
	MemberRoleIDs []string
	CanonicalName string
	InvokedName   string
}

type CheckResult struct {
	Outcome ExecutionOutcome
	Reason  ExecutionReason
}

func (d CheckResult) Allowed() bool {
	return d.Outcome == OutcomeAllow
}

type AdmissionOutcome string

const (
	AdmissionAllowed     AdmissionOutcome = "allowed"
	AdmissionRestricted  AdmissionOutcome = "restricted"
	AdmissionUnavailable AdmissionOutcome = "unavailable"
)

type AdmissionResult struct {
	Outcome AdmissionOutcome
}

func (d AdmissionResult) Restricted() bool {
	return d.Outcome == AdmissionRestricted
}

func (d AdmissionResult) Unavailable() bool {
	return d.Outcome == AdmissionUnavailable
}

func (d AdmissionResult) Allowed() bool {
	return d.Outcome == AdmissionAllowed
}

func (m *Module) CanExecute(req ExecutionRequest) CheckResult {
	if m == nil || m.db == nil {
		return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
	}

	if req.GuildID != "" {
		isBL, err := m.db.IsGlobalBlacklisted(req.GuildID)
		if err != nil {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
		}
		if isBL {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonGlobalGuildRestricted}
		}
	}

	if req.UserID != "" {
		isBL, err := m.db.IsGlobalBlacklisted(req.UserID)
		if err != nil {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
		}
		if isBL {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonGlobalUserRestricted}
		}

		if _, ok := m.configuredUsers[req.UserID]; ok {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonConfiguredGlobalUserRestricted}
		}
	}

	canon := strings.ToLower(strings.TrimSpace(req.CanonicalName))
	invoked := strings.ToLower(strings.TrimSpace(req.InvokedName))

	if req.GuildID == "" {
		if canon != "" {
			disabled, err := m.db.IsCommandDisabled("", canon)
			if err != nil {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
			}
			if disabled {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonCanonicalCommandDisabled}
			}
		}
		if invoked != "" && invoked != canon {
			disabled, err := m.db.IsCommandDisabled("", invoked)
			if err != nil {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
			}
			if disabled {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonInvokedCommandDisabled}
			}
		}
		return CheckResult{Outcome: OutcomeAllow, Reason: ReasonAllowed}
	}

	if canon != "" {
		disabled, err := m.db.IsCommandDisabled(req.GuildID, canon)
		if err != nil {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
		}
		if disabled {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonCanonicalCommandDisabled}
		}
	}

	restr, err := m.db.GetGuildRestrictions(req.GuildID)
	if err != nil {
		return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
	}

	if req.ChannelID != "" && restr != nil {
		for _, chID := range restr.ChannelIDs {
			if chID == req.ChannelID {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonGuildChannelRestricted}
			}
		}
	}

	if req.UserID != "" && restr != nil {
		for _, uID := range restr.UserIDs {
			if uID == req.UserID {
				return CheckResult{Outcome: OutcomeDeny, Reason: ReasonGuildUserRestricted}
			}
		}
	}

	if len(req.MemberRoleIDs) > 0 && restr != nil && len(restr.RoleIDs) > 0 {
		for _, blRole := range restr.RoleIDs {
			for _, memberRole := range req.MemberRoleIDs {
				if memberRole == blRole {
					return CheckResult{Outcome: OutcomeDeny, Reason: ReasonGuildRoleRestricted}
				}
			}
		}
	}

	if invoked != "" && invoked != canon {
		disabled, err := m.db.IsCommandDisabled(req.GuildID, invoked)
		if err != nil {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonUnavailable}
		}
		if disabled {
			return CheckResult{Outcome: OutcomeDeny, Reason: ReasonInvokedCommandDisabled}
		}
	}

	return CheckResult{Outcome: OutcomeAllow, Reason: ReasonAllowed}
}

func (m *Module) DecideUserAdmission(userID string) AdmissionResult {
	if m == nil || m.db == nil || userID == "" {
		return AdmissionResult{Outcome: AdmissionUnavailable}
	}
	if isBL, err := m.db.IsGlobalBlacklisted(userID); err != nil {
		return AdmissionResult{Outcome: AdmissionUnavailable}
	} else if isBL {
		return AdmissionResult{Outcome: AdmissionRestricted}
	}
	if _, ok := m.configuredUsers[userID]; ok {
		return AdmissionResult{Outcome: AdmissionRestricted}
	}
	return AdmissionResult{Outcome: AdmissionAllowed}
}

func (m *Module) IsGuildBlocked(guildID string) AdmissionResult {
	if m == nil || m.db == nil || guildID == "" {
		return AdmissionResult{Outcome: AdmissionUnavailable}
	}
	isBL, err := m.db.IsGlobalBlacklisted(guildID)
	if err != nil {
		return AdmissionResult{Outcome: AdmissionUnavailable}
	}
	if isBL {
		return AdmissionResult{Outcome: AdmissionRestricted}
	}
	return AdmissionResult{Outcome: AdmissionAllowed}
}
