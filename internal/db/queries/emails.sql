-- name: InsertEmailDelivery :one
INSERT INTO email_deliveries (kind, to_addrs, cc_addrs, subject, html_body, text_body)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetEmailDelivery :one
SELECT * FROM email_deliveries WHERE id = $1;

-- name: IncrementEmailDeliveryAttempts :exec
UPDATE email_deliveries SET attempts = attempts + 1 WHERE id = $1 AND status = 'pending';

-- name: MarkEmailDeliverySent :execrows
UPDATE email_deliveries
SET status = 'sent', sent_at = now(), provider_message_id = $2, last_error = NULL
WHERE id = $1 AND status = 'pending';

-- name: MarkEmailDeliveryFailed :execrows
UPDATE email_deliveries SET status = 'failed', last_error = $2 WHERE id = $1 AND status = 'pending';

-- name: SetEmailDeliveryError :exec
UPDATE email_deliveries SET last_error = $2 WHERE id = $1 AND status = 'pending';
