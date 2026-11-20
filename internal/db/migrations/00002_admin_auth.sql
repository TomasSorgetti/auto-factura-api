-- +goose Up
CREATE TABLE admin_users (
	id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	email               text NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
	password_hash       text NOT NULL,
	totp_secret_enc     bytea,
	totp_enabled_at     timestamptz,
	totp_last_step      bigint,
	sessions_revoked_at timestamptz,
	password_changed_at timestamptz NOT NULL DEFAULT now(),
	created_at          timestamptz NOT NULL DEFAULT now(),
	updated_at          timestamptz NOT NULL DEFAULT now(),
	CHECK ((totp_enabled_at IS NULL) OR (totp_secret_enc IS NOT NULL))
);
CREATE UNIQUE INDEX admin_users_singleton ON admin_users ((true));
CREATE UNIQUE INDEX admin_users_email_key ON admin_users (email);
CREATE TRIGGER admin_users_set_updated_at BEFORE UPDATE ON admin_users
	FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE admin_refresh_tokens (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id        uuid NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
	jti            uuid NOT NULL UNIQUE,
	token_hash     bytea NOT NULL CHECK (length(token_hash) = 32),
	ip             text NOT NULL DEFAULT '' CHECK (length(ip) <= 64),
	user_agent     text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 500),
	expires_at     timestamptz NOT NULL,
	revoked_at     timestamptz,
	replaced_by_id uuid REFERENCES admin_refresh_tokens (id) ON DELETE SET NULL,
	created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_refresh_tokens_user_id_idx ON admin_refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX admin_refresh_tokens_replaced_by_id_idx ON admin_refresh_tokens (replaced_by_id);
CREATE INDEX admin_refresh_tokens_expires_at_idx ON admin_refresh_tokens (expires_at);

CREATE TABLE admin_recovery_codes (
	id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id    uuid NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
	code_hash  bytea NOT NULL UNIQUE CHECK (length(code_hash) = 32),
	used_at    timestamptz,
	created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_recovery_codes_user_id_idx ON admin_recovery_codes (user_id);

CREATE TABLE admin_login_devices (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id       uuid NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
	device_hash   bytea NOT NULL CHECK (length(device_hash) = 32),
	user_agent    text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 500),
	last_ip       text NOT NULL DEFAULT '' CHECK (length(last_ip) <= 64),
	first_seen_at timestamptz NOT NULL DEFAULT now(),
	last_seen_at  timestamptz NOT NULL DEFAULT now(),
	alerted_at    timestamptz,
	UNIQUE (user_id, device_hash)
);

-- +goose Down
DROP TABLE admin_login_devices;
DROP TABLE admin_recovery_codes;
DROP TABLE admin_refresh_tokens;
DROP TABLE admin_users;
