-- name: GetSettings :one
SELECT * FROM settings WHERE id;

-- name: GetSettingsForUpdate :one
SELECT * FROM settings WHERE id FOR UPDATE;

-- name: UpsertSettings :one
INSERT INTO settings (id, cuit, legal_name, address, gross_income_number, activity_start_date, point_of_sale, monotributo_category, annual_cap_cents)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
	cuit = EXCLUDED.cuit,
	legal_name = EXCLUDED.legal_name,
	address = EXCLUDED.address,
	gross_income_number = EXCLUDED.gross_income_number,
	activity_start_date = EXCLUDED.activity_start_date,
	point_of_sale = EXCLUDED.point_of_sale,
	monotributo_category = EXCLUDED.monotributo_category,
	annual_cap_cents = EXCLUDED.annual_cap_cents
RETURNING *;

-- name: SetArcaEnv :one
UPDATE settings SET arca_env = $1 WHERE id RETURNING *;
