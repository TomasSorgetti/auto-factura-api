-- +goose Up
CREATE TABLE settings (
	id                   boolean PRIMARY KEY DEFAULT true CHECK (id),
	cuit                 text NOT NULL CHECK (cuit ~ '^[0-9]{11}$'),
	legal_name           text NOT NULL CHECK (length(btrim(legal_name)) BETWEEN 1 AND 200),
	address              text NOT NULL CHECK (length(btrim(address)) BETWEEN 1 AND 300),
	gross_income_number  text NOT NULL CHECK (length(btrim(gross_income_number)) BETWEEN 1 AND 30),
	activity_start_date  date NOT NULL,
	point_of_sale        integer NOT NULL CHECK (point_of_sale BETWEEN 1 AND 99999),
	monotributo_category text NOT NULL CHECK (monotributo_category IN ('A','B','C','D','E','F','G','H','I','J','K')),
	annual_cap_cents     bigint NOT NULL CHECK (annual_cap_cents > 0 AND annual_cap_cents <= 100000000000000),
	arca_env             text NOT NULL DEFAULT 'homologation' CHECK (arca_env IN ('homologation', 'production')),
	created_at           timestamptz NOT NULL DEFAULT now(),
	updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER settings_updated_at BEFORE UPDATE ON settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE settings;
