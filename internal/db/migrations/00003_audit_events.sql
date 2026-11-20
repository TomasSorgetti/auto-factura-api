-- +goose Up
CREATE TABLE audit_events (
	id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	occurred_at timestamptz NOT NULL DEFAULT now(),
	actor_type  text NOT NULL CHECK (actor_type IN ('admin', 'system', 'portal', 'cli', 'anonymous')),
	actor_id    uuid,
	action      text NOT NULL CHECK (action ~ '^[a-z_]+\.[a-z_]+$'),
	entity_type text CHECK (entity_type ~ '^[a-z_]+$'),
	entity_id   text CHECK (length(entity_id) <= 64),
	ip          inet,
	request_id  text CHECK (length(request_id) <= 64),
	data        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
	CHECK ((actor_type IN ('admin', 'portal')) = (actor_id IS NOT NULL))
);
CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX audit_events_entity_idx ON audit_events (entity_type, entity_id, occurred_at DESC) WHERE entity_id IS NOT NULL;
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	RAISE EXCEPTION 'audit_events is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
	FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

-- +goose Down
DROP TABLE audit_events;
DROP FUNCTION audit_events_append_only();
