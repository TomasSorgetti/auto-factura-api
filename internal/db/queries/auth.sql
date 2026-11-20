-- name: CreateAdminUser :one
INSERT INTO admin_users (email, password_hash) VALUES ($1, $2) RETURNING *;

-- name: GetAdminUser :one
SELECT * FROM admin_users LIMIT 1;

-- name: GetAdminUserByID :one
SELECT * FROM admin_users WHERE id = $1;

-- name: GetAdminUserByEmail :one
SELECT * FROM admin_users WHERE email = $1;

-- name: GetAdminSessionState :one
SELECT id, sessions_revoked_at FROM admin_users WHERE id = $1;

-- name: LockAdminSessionState :one
SELECT sessions_revoked_at FROM admin_users WHERE id = $1 FOR UPDATE;

-- name: SetAdminPassword :exec
UPDATE admin_users SET password_hash = $2, password_changed_at = now() WHERE id = $1;

-- name: EnableAdminTOTP :execrows
UPDATE admin_users SET totp_secret_enc = $2, totp_enabled_at = now(), totp_last_step = $3
WHERE id = $1 AND totp_enabled_at IS NULL;

-- name: AdvanceAdminTOTPStep :execrows
UPDATE admin_users SET totp_last_step = $2
WHERE id = $1 AND totp_enabled_at IS NOT NULL AND (totp_last_step IS NULL OR totp_last_step < $2);

-- name: ResetAdminTOTP :exec
UPDATE admin_users SET totp_secret_enc = NULL, totp_enabled_at = NULL, totp_last_step = NULL WHERE id = $1;

-- name: RevokeAdminSessions :exec
UPDATE admin_users SET sessions_revoked_at = $2 WHERE id = $1;

-- name: CreateRefreshToken :one
INSERT INTO admin_refresh_tokens (user_id, jti, token_hash, ip, user_agent, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id;

-- name: GetRefreshTokenByJTIForUpdate :one
SELECT * FROM admin_refresh_tokens WHERE jti = $1 FOR UPDATE;

-- name: GetRefreshTokenByID :one
SELECT * FROM admin_refresh_tokens WHERE id = $1;

-- name: RotateRefreshToken :exec
UPDATE admin_refresh_tokens SET revoked_at = $2, replaced_by_id = $3 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshToken :exec
UPDATE admin_refresh_tokens SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokens :exec
UPDATE admin_refresh_tokens SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM admin_refresh_tokens WHERE expires_at < $1;

-- name: DeleteRecoveryCodes :exec
DELETE FROM admin_recovery_codes WHERE user_id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO admin_recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: UseRecoveryCode :execrows
UPDATE admin_recovery_codes SET used_at = now()
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM admin_recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: CountLoginDevices :one
SELECT count(*) FROM admin_login_devices WHERE user_id = $1;

-- name: TouchLoginDevice :one
INSERT INTO admin_login_devices (user_id, device_hash, user_agent, last_ip)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, device_hash)
DO UPDATE SET last_seen_at = now(), user_agent = EXCLUDED.user_agent, last_ip = EXCLUDED.last_ip
RETURNING id, (xmax = 0)::bool AS is_new;

-- name: LastLoginDeviceAlertAt :one
SELECT max(alerted_at)::timestamptz FROM admin_login_devices WHERE user_id = $1;

-- name: MarkLoginDeviceAlerted :exec
UPDATE admin_login_devices SET alerted_at = $2 WHERE id = $1;
