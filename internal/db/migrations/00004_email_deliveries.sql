-- +goose Up
CREATE TABLE email_deliveries (
	id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	kind                text NOT NULL CHECK (kind IN ('device_alert')),
	to_addrs            text[] NOT NULL CHECK (cardinality(to_addrs) BETWEEN 1 AND 50),
	cc_addrs            text[] NOT NULL DEFAULT '{}' CHECK (cardinality(cc_addrs) <= 50),
	subject             text NOT NULL CHECK (length(subject) BETWEEN 1 AND 998),
	html_body           text NOT NULL CHECK (octet_length(html_body) <= 524288),
	text_body           text NOT NULL CHECK (octet_length(text_body) <= 524288),
	status              text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
	attempts            integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
	last_error          text CHECK (length(last_error) <= 500),
	provider_message_id text,
	sent_at             timestamptz,
	created_at          timestamptz NOT NULL DEFAULT now(),
	updated_at          timestamptz NOT NULL DEFAULT now(),
	CHECK ((status = 'sent') = (sent_at IS NOT NULL))
);
CREATE INDEX email_deliveries_created_idx ON email_deliveries (created_at DESC, id DESC);
CREATE INDEX email_deliveries_open_idx ON email_deliveries (status) WHERE status <> 'sent';
CREATE TRIGGER email_deliveries_updated_at BEFORE UPDATE ON email_deliveries
	FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE email_deliveries;
