package nowplaying

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"time"
)

// The media-control stream doesn't hear about every change. MusicKit's
// out-of-process player (what dj plays albums through) never pushes track
// changes, and its artwork lands a beat after the track starts. Rather than
// poll, fetch a snapshot only when something is likely to have changed: just
// after the current track should have ended, while artwork is missing, and
// after our own next/previous track dial.
var (
	endChecks = []time.Duration{750 * time.Millisecond, 3 * time.Second} // past expected end
	artChecks = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
)

const keyCheck = time.Second

// reconciler is owned by the reconcileLoop goroutine.
type reconciler struct {
	track    string // trackKey the counters below belong to
	seen     time.Time
	endTries int
	artTries int
	soon     time.Time // a requested check, e.g. after a key press
}

// deadlines returns when each kind of check is next due; zero means not due.
func (r *reconciler) deadlines(np NowPlaying, now time.Time) (soon, end, art time.Time) {
	if k := np.trackKey(); k != r.track {
		*r = reconciler{track: k, seen: now, soon: r.soon}
	}
	if np.Playing && np.DurationMicros > 0 && r.endTries < len(endChecks) {
		trackEnd := time.UnixMicro(np.TimestampEpochMicros + np.DurationMicros - np.ElapsedTimeMicros)
		end = trackEnd.Add(endChecks[r.endTries])
	}
	if np.Title != "" && np.ArtworkData == "" && r.artTries < len(artChecks) {
		art = r.seen.Add(artChecks[r.artTries])
	}
	return r.soon, end, art
}

// next returns the earliest due check, and whether any is due at all.
func (r *reconciler) next(np NowPlaying, now time.Time) (time.Time, bool) {
	var at time.Time
	soon, end, art := r.deadlines(np, now)
	for _, t := range []time.Time{soon, end, art} {
		if !t.IsZero() && (at.IsZero() || t.Before(at)) {
			at = t
		}
	}
	return at, !at.IsZero()
}

// fired marks every check due by now as done, so each one runs once.
func (r *reconciler) fired(np NowPlaying, now time.Time) {
	soon, end, art := r.deadlines(np, now)
	due := func(t time.Time) bool { return !t.IsZero() && !t.After(now) }
	if due(soon) {
		r.soon = time.Time{}
	}
	if due(end) {
		r.endTries++
	}
	if due(art) {
		r.artTries++
	}
}

// reconcileLoop fetches snapshots as the reconciler schedules them. A send on
// m.wake makes it reschedule; a positive duration also asks for a check then.
func (m *Module) reconcileLoop(ctx context.Context) {
	var r reconciler
	for {
		var timer <-chan time.Time
		if at, ok := r.next(m.liveState.get(), time.Now()); ok {
			timer = time.After(time.Until(at))
		}
		select {
		case <-ctx.Done():
			return
		case d := <-m.wake:
			if d > 0 {
				r.soon = time.Now().Add(d)
			}
		case <-timer:
			r.fired(m.liveState.get(), time.Now())
			m.fetchSnapshot(ctx)
		}
	}
}

// requestCheck asks the reconciler to reschedule, and to take a snapshot
// after d if d is positive. It never blocks.
func (m *Module) requestCheck(d time.Duration) {
	select {
	case m.wake <- d:
	default:
	}
}

// fetchSnapshot replaces the live state with a full `media-control get`.
func (m *Module) fetchSnapshot(ctx context.Context) {
	out, err := exec.CommandContext(ctx, "media-control", "get", "--micros").Output()
	if err != nil {
		log.Printf("media-control get failed: %v", err)
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil || payload == nil {
		return // "null" means nothing is playing; the stream reports that fine
	}
	var np NowPlaying
	mergePayloadMap(&np, payload)

	m.liveState.Lock()
	if np.trackKey() != m.liveState.trackKey() {
		log.Printf("Snapshot caught a change the stream missed: %s - %s", np.Artist, np.Title)
	}
	m.liveState.NowPlaying = np
	m.liveState.Unlock()
}
