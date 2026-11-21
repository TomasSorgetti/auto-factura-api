package audit

const (
	ActionLoginSucceeded   Action = "auth.login_succeeded"
	ActionLoginFailed      Action = "auth.login_failed"
	ActionNewDevice        Action = "auth.new_device"
	ActionLogout           Action = "auth.logout"
	ActionRefreshReplay    Action = "auth.refresh_replay"
	ActionTOTPEnrolled     Action = "auth.totp_enrolled"
	ActionRecoveryCodeUsed Action = "auth.recovery_code_used"
	ActionUserCreated      Action = "auth.user_created"
	ActionPasswordReset    Action = "auth.password_reset"
	ActionTOTPReset        Action = "auth.totp_reset"
	ActionSessionsRevoked  Action = "auth.sessions_revoked"
	ActionSettingsUpdated  Action = "settings.updated"
	ActionArcaEnvChanged   Action = "settings.arca_env_changed"
	ActionClientCreated    Action = "client.created"
	ActionClientUpdated    Action = "client.updated"
	ActionClientArchived   Action = "client.archived"
	ActionClientUnarchived Action = "client.unarchived"
	ActionClientDeleted    Action = "client.deleted"
	ActionContactCreated   Action = "client.contact_created"
	ActionContactUpdated   Action = "client.contact_updated"
	ActionContactDeleted   Action = "client.contact_deleted"
)

func (a Action) valid() bool {
	switch a {
	case ActionLoginSucceeded, ActionLoginFailed, ActionNewDevice, ActionLogout, ActionRefreshReplay,
		ActionTOTPEnrolled, ActionRecoveryCodeUsed, ActionUserCreated, ActionPasswordReset, ActionTOTPReset,
		ActionSessionsRevoked, ActionSettingsUpdated, ActionArcaEnvChanged,
		ActionClientCreated, ActionClientUpdated, ActionClientArchived, ActionClientUnarchived, ActionClientDeleted,
		ActionContactCreated, ActionContactUpdated, ActionContactDeleted:
		return true
	}
	return false
}
