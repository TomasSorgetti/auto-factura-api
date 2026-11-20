-- name: InsertAuditEvent :exec
INSERT INTO audit_events (actor_type, actor_id, action, entity_type, entity_id, ip, request_id, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
