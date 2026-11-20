// Package audit records business and security events in audit_events. Events are never updated or deleted.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

const (
	maxDataKeys     = 20
	maxDataValueLen = 500
)

type ActorType string

const (
	ActorAdmin     ActorType = "admin"
	ActorSystem    ActorType = "system"
	ActorPortal    ActorType = "portal"
	ActorCLI       ActorType = "cli"
	ActorAnonymous ActorType = "anonymous"
)

type Actor struct {
	Type ActorType
	// ID is the admin user or portal access UUID; required for admin and portal, empty otherwise.
	ID string
	// IP is empty when unknown.
	IP string
}

type Action string

type Event struct {
	Actor      Actor
	Action     Action
	EntityType string
	EntityID   string
	// Data must never hold secrets, tokens, passwords or codes.
	Data map[string]string
}

var ErrInvalidEvent = errors.New("invalid audit event")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidEvent, fmt.Sprintf(format, args...))
}

func (e Event) validate() (pgtype.UUID, *netip.Addr, error) {
	var id pgtype.UUID

	needsID := false
	switch e.Actor.Type {
	case ActorAdmin, ActorPortal:
		needsID = true
	case ActorSystem, ActorCLI, ActorAnonymous:
	default:
		return id, nil, invalid("unknown actor type %q", e.Actor.Type)
	}
	switch {
	case needsID && e.Actor.ID == "":
		return id, nil, invalid("actor %s requires an id", e.Actor.Type)
	case !needsID && e.Actor.ID != "":
		return id, nil, invalid("actor %s must not have an id", e.Actor.Type)
	case needsID:
		if err := id.Scan(e.Actor.ID); err != nil {
			return id, nil, invalid("actor id is not a uuid")
		}
	}
	if !e.Action.valid() {
		return id, nil, invalid("unknown action %q", e.Action)
	}
	if len(e.Data) > maxDataKeys {
		return id, nil, invalid("data has more than %d keys", maxDataKeys)
	}
	for k, v := range e.Data {
		if len(v) > maxDataValueLen {
			return id, nil, invalid("data value for %q exceeds %d bytes", k, maxDataValueLen)
		}
	}
	var ip *netip.Addr
	if e.Actor.IP != "" {
		addr, err := netip.ParseAddr(e.Actor.IP)
		if err != nil {
			return id, nil, invalid("ip is not valid")
		}
		ip = &addr
	}
	return id, ip, nil
}

// Record inserts e with db, which is the pool or the transaction of the change being audited.
// The request id comes from ctx.
func Record(ctx context.Context, db sqlc.DBTX, e Event) error {
	actorID, ip, err := e.validate()
	if err != nil {
		return err
	}
	data := e.Data
	if data == nil {
		data = map[string]string{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal audit data: %w", err)
	}
	params := sqlc.InsertAuditEventParams{
		ActorType:  string(e.Actor.Type),
		ActorID:    actorID,
		Action:     string(e.Action),
		EntityType: textOrNil(e.EntityType),
		EntityID:   textOrNil(e.EntityID),
		Ip:         ip,
		RequestID:  textOrNil(logx.RequestID(ctx)),
		Data:       raw,
	}
	if err := sqlc.New(db).InsertAuditEvent(ctx, params); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
