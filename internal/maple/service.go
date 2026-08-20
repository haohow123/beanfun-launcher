package maple

import (
	"context"
	"net/http"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// statusHeartbeatName is the bgtask registry key for the
// status-probe heartbeat. App-lifecycle scoped — registered at
// service construction, stopped via bgtask.StopAll() at shutdown.
const statusHeartbeatName = "maple-server-status"

// statusInterval is how often the heartbeat re-probes game
// servers. 2 minutes is conservative: server up/down state changes
// rarely (usually aligned with announced maintenance windows), and
// we want minimal network footprint on Gamania's edge. Shorter
// makes the UI marginally more responsive to a real outage; longer
// is fine since the Hero indicator is informational only, not
// blocking the user.
const statusInterval = 2 * time.Minute

// StatusChangedEvent is the Wails event carrying a Status snapshot on every Online flip; the
// frontend bridge in frontend/src/queries/mapleStatus.ts must use the same string.
const StatusChangedEvent = "maple:status-changed"

// MapleService is the Wails-bound facade for MapleStory-specific
// data. Currently exposes server status only; future expansion
// (event list, character info) hangs here. Constructed once in
// main.go and registered as a Wails service so the frontend can
// call MapleService.ServerStatus().
type MapleService struct {
	checker *Checker
}

// NewMapleService creates the service and immediately registers
// its background status-probe heartbeat against mgr. firstDelay=0
// means the first probe runs without waiting, so the Hero
// indicator shows a real value within ~3 s of app start instead
// of staying "checking…" for the full 2 min interval.
//
// httpClient is used only for the self-network canary HEAD on
// tw.beanfun.com; pass nil to fall back to http.DefaultClient.
// We don't need cookie continuity with the Beanfun login client
// — the canary is just "can we reach Gamania's web tier".
//
// onStatusChanged, if non-nil, receives a Status snapshot on the
// initial probe and on every Online flip (see
// Checker.OnStatusChanged); installed before the heartbeat for the
// same reason as onServerOnline.
//
// onServerOnline, if non-nil, is invoked when the heartbeat
// detects an offline → online state transition (not on the
// initial probe; see Checker.OnServerOnline). Installed BEFORE
// the heartbeat is registered so the first probe's eventual
// transition-fire can never see a nil hook.
func NewMapleService(
	mgr *bgtask.Manager,
	httpClient *http.Client,
	onServerOnline func(),
	onStatusChanged func(Status),
) *MapleService {
	s := &MapleService{checker: NewChecker(httpClient)}
	s.checker.OnServerOnline = onServerOnline
	s.checker.OnStatusChanged = onStatusChanged
	mgr.Heartbeat(statusHeartbeatName, 0, func(ctx context.Context) time.Duration {
		s.checker.CheckStatus(ctx)
		return statusInterval
	})
	return s
}

// ServerStatus returns the latest cached probe result. Called by
// the frontend via Wails IPC; the actual probe runs in the
// background heartbeat goroutine (see NewMapleService).
func (s *MapleService) ServerStatus() Status {
	return s.checker.Status()
}
