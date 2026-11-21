-- name: CreateClient :one
INSERT INTO clients (id, legal_name, cuit, tax_condition, address, notes_enc)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetClient :one
SELECT * FROM clients WHERE id = $1;

-- name: GetClientForUpdate :one
SELECT * FROM clients WHERE id = $1 FOR UPDATE;

-- name: UpdateClient :one
UPDATE clients SET legal_name = $2, cuit = $3, tax_condition = $4, address = $5, notes_enc = $6
WHERE id = $1 AND archived_at IS NULL
RETURNING *;

-- name: ArchiveClient :one
UPDATE clients SET archived_at = now() WHERE id = $1 AND archived_at IS NULL RETURNING *;

-- name: UnarchiveClient :one
UPDATE clients SET archived_at = NULL WHERE id = $1 AND archived_at IS NOT NULL RETURNING *;

-- name: ListClients :many
SELECT c.id, c.legal_name, c.cuit, c.tax_condition, c.archived_at,
	(SELECT count(*) FROM client_contacts cc WHERE cc.client_id = c.id)::int AS contacts_count
FROM clients c
WHERE (c.archived_at IS NOT NULL) = sqlc.arg(archived)::boolean
	AND (sqlc.narg(cuit_prefix)::text IS NULL OR c.cuit LIKE sqlc.narg(cuit_prefix)::text || '%')
	AND (sqlc.narg(name_pattern)::text IS NULL OR c.legal_name ILIKE sqlc.narg(name_pattern)::text ESCAPE '\')
ORDER BY lower(c.legal_name), c.id
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountClients :one
SELECT count(*)::int FROM clients c
WHERE (c.archived_at IS NOT NULL) = sqlc.arg(archived)::boolean
	AND (sqlc.narg(cuit_prefix)::text IS NULL OR c.cuit LIKE sqlc.narg(cuit_prefix)::text || '%')
	AND (sqlc.narg(name_pattern)::text IS NULL OR c.legal_name ILIKE sqlc.narg(name_pattern)::text ESCAPE '\');

-- name: ListContacts :many
SELECT * FROM client_contacts WHERE client_id = $1 ORDER BY created_at, id LIMIT 20;

-- name: CountContacts :one
SELECT count(*)::int FROM client_contacts WHERE client_id = $1;

-- name: GetContact :one
SELECT * FROM client_contacts WHERE id = $1 AND client_id = $2;

-- name: CreateContact :one
INSERT INTO client_contacts (id, client_id, name_enc, email_enc, email_hmac, role, portal_access)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateContact :one
UPDATE client_contacts SET name_enc = $3, email_enc = $4, email_hmac = $5, role = $6, portal_access = $7
WHERE id = $1 AND client_id = $2
RETURNING *;

-- name: DeleteContact :execrows
DELETE FROM client_contacts WHERE id = $1 AND client_id = $2;
