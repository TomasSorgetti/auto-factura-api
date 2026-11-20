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
)

func (a Action) valid() bool {
	switch a {
	case ActionLoginSucceeded, ActionLoginFailed, ActionNewDevice, ActionLogout, ActionRefreshReplay,
		ActionTOTPEnrolled, ActionRecoveryCodeUsed, ActionUserCreated, ActionPasswordReset, ActionTOTPReset,
		ActionSessionsRevoked, ActionSettingsUpdated, ActionArcaEnvChanged:
		return true
	}
	return false
}
