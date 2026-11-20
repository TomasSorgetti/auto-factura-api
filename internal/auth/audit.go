package auth

import (
	"context"
	"log/slog"
	"net/netip"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
)

// Auditor records security events. It is best effort: a failure is logged and never reaches the caller.
type Auditor interface {
	record(ctx context.Context, e audit.Event)
}

// SecurityAuditor writes events to audit_events through db, outside any business transaction.
type SecurityAuditor struct {
	db  sqlc.DBTX
	log *slog.Logger
}

func NewSecurityAuditor(db sqlc.DBTX, log *slog.Logger) *SecurityAuditor {
	return &SecurityAuditor{db: db, log: log}
}

func (a *SecurityAuditor) record(ctx context.Context, e audit.Event) {
	if err := audit.Record(ctx, a.db, e); err != nil {
		a.log.WarnContext(ctx, "record audit event", "action", string(e.Action), "err", err)
	}
}

func recordEvent(ctx context.Context, a Auditor, e audit.Event) {
	if a != nil {
		a.record(ctx, e)
	}
}

// auditIP returns ip when it parses, and "" otherwise, so an odd proxy header never makes an audit event invalid.
func auditIP(ip string) string {
	if _, err := netip.ParseAddr(ip); err != nil {
		return ""
	}
	return ip
}

func adminActor(userID, ip string) audit.Actor {
	return audit.Actor{Type: audit.ActorAdmin, ID: userID, IP: auditIP(ip)}
}

var cliActor = audit.Actor{Type: audit.ActorCLI}

func anonymousActor(ip string) audit.Actor {
	return audit.Actor{Type: audit.ActorAnonymous, IP: auditIP(ip)}
}
