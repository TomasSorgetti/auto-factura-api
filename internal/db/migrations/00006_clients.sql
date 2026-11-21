-- +goose Up
CREATE TABLE clients (
	id            uuid PRIMARY KEY,
	legal_name    text NOT NULL CHECK (length(btrim(legal_name)) BETWEEN 1 AND 200),
	cuit          text NOT NULL UNIQUE CHECK (cuit ~ '^[0-9]{11}$'),
	tax_condition text NOT NULL CHECK (tax_condition IN ('registered', 'monotributo', 'exempt', 'final_consumer')),
	address       text NOT NULL CHECK (length(btrim(address)) BETWEEN 1 AND 300),
	notes_enc     bytea CHECK (length(notes_enc) BETWEEN 29 AND 8192),
	archived_at   timestamptz,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX clients_name_idx ON clients (lower(legal_name), id);
CREATE TRIGGER clients_updated_at BEFORE UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE client_contacts (
	id            uuid PRIMARY KEY,
	client_id     uuid NOT NULL REFERENCES clients (id) ON DELETE RESTRICT,
	name_enc      bytea NOT NULL CHECK (length(name_enc) BETWEEN 29 AND 1024),
	email_enc     bytea NOT NULL CHECK (length(email_enc) BETWEEN 29 AND 1024),
	email_hmac    bytea NOT NULL CHECK (length(email_hmac) = 32),
	role          text NOT NULL CHECK (role IN ('to', 'cc')),
	portal_access boolean NOT NULL DEFAULT false,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	UNIQUE (client_id, email_hmac)
);
CREATE TRIGGER client_contacts_updated_at BEFORE UPDATE ON client_contacts FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE client_contacts;
DROP TABLE clients;
