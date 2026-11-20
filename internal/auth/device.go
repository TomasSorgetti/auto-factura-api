package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const (
	deviceAlertCooldown = 10 * time.Minute
	DeviceCookieMaxAge  = 400 * 24 * time.Hour
	deviceTrackTimeout  = 20 * time.Second
)

func isDeviceID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// shouldAlertNewDevice reports whether a login warrants an email. The first
// device ever seen never alerts, and alerts are spaced by deviceAlertCooldown.
func shouldAlertNewDevice(isNew, hadPrevious bool, lastAlertAt *time.Time, now time.Time) bool {
	return isNew && hadPrevious && (lastAlertAt == nil || now.Sub(*lastAlertAt) >= deviceAlertCooldown)
}

// describeUserAgent returns e.g. "Chrome en Windows"; unknown parts become "Navegador desconocido" / "sistema desconocido".
func describeUserAgent(ua string) string {
	browser := "Navegador desconocido"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	system := "sistema desconocido"
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		system = "iOS"
	case strings.Contains(ua, "Android"):
		system = "Android"
	case strings.Contains(ua, "Windows"):
		system = "Windows"
	case strings.Contains(ua, "Mac OS X"):
		system = "macOS"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	return browser + " en " + system
}

type DeviceTrackerDeps struct {
	Store   *Store
	Outbox  *emails.Outbox
	Log     *slog.Logger
	Now     func() time.Time
	Loc     *time.Location
	BaseURL string
}

// DeviceTracker records the device of each login and emails an alert for a new one.
type DeviceTracker struct {
	store   *Store
	outbox  *emails.Outbox
	log     *slog.Logger
	now     func() time.Time
	loc     *time.Location
	baseURL string
}

func NewDeviceTracker(d DeviceTrackerDeps) (*DeviceTracker, error) {
	if d.Store == nil || d.Outbox == nil || d.Log == nil || d.Loc == nil {
		return nil, errors.New("device tracker: store, outbox, log and location are required")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &DeviceTracker{store: d.Store, outbox: d.Outbox, log: d.Log, now: now, loc: d.Loc, baseURL: d.BaseURL}, nil
}

// Track records the device and may email an alert. It never returns an error: failures are logged.
// It runs detached from ctx cancellation so a client that disconnects cannot leave a device half recorded.
func (d *DeviceTracker) Track(ctx context.Context, userID, email string, meta ClientMeta, method LoginMethod) (deviceID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deviceTrackTimeout)
	defer cancel()
	deviceID = meta.DeviceID
	if !isDeviceID(deviceID) {
		id, err := newUUID()
		if err != nil {
			d.log.WarnContext(ctx, "track login device", "err", err)
			return ""
		}
		deviceID = id.String()
	}
	if err := d.track(ctx, userID, email, deviceID, meta, method); err != nil {
		d.log.WarnContext(ctx, "track login device", "err", err)
	}
	return deviceID
}

func (d *DeviceTracker) track(ctx context.Context, userID, email, deviceID string, meta ClientMeta, method LoginMethod) error {
	uid, err := parseUUID(userID)
	if err != nil {
		return err
	}
	return d.store.InTx(ctx, func(st *Store) error {
		count, err := st.countLoginDevices(ctx, uid)
		if err != nil {
			return err
		}
		id, isNew, err := st.touchLoginDevice(ctx, uid, secrets.HashToken(deviceID), meta.UserAgent, meta.IP)
		if err != nil {
			return err
		}
		if !isNew || count == 0 {
			return nil
		}
		last, err := st.lastLoginDeviceAlertAt(ctx, uid)
		if err != nil {
			return err
		}
		now := d.now()
		if !shouldAlertNewDevice(isNew, count > 0, last, now) {
			return nil
		}
		device := describeUserAgent(meta.UserAgent)
		msg, err := newDeviceMessage(email, alertData{
			When:     now.In(d.loc),
			Device:   device,
			IP:       meta.IP,
			Method:   method,
			LoginURL: strings.TrimRight(d.baseURL, "/") + "/login",
		})
		if err != nil {
			return err
		}
		if _, err := d.outbox.Enqueue(ctx, st.txn(), msg); err != nil {
			return err
		}
		if err := st.markLoginDeviceAlerted(ctx, id, now); err != nil {
			return err
		}
		return audit.Record(ctx, st.txn(), audit.Event{
			Actor:  adminActor(userID, meta.IP),
			Action: audit.ActionNewDevice,
			Data:   map[string]string{"device": device},
		})
	})
}
