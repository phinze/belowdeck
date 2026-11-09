package nowplaying

import (
	"testing"
	"time"
)

func TestReconcilerChecksTrackEndOnceEach(t *testing.T) {
	now := time.Now()
	np := NowPlaying{
		Title: "Airbag", ArtworkData: "art", Playing: true,
		DurationMicros:       10_000_000,
		ElapsedTimeMicros:    4_000_000,
		TimestampEpochMicros: now.UnixMicro(),
	}
	var r reconciler

	for i, offset := range endChecks {
		at, ok := r.next(np, now)
		want := now.Add(6*time.Second + offset)
		if !ok || at.Sub(want).Abs() > 10*time.Millisecond {
			t.Fatalf("check %d: due %v (ok=%v), want %v", i, at.Sub(now), ok, want.Sub(now))
		}
		r.fired(np, at)
	}
	if at, ok := r.next(np, now); ok {
		t.Fatalf("checks exhausted but another due in %v", at.Sub(now))
	}

	np.Title, np.TimestampEpochMicros = "Paranoid Android", now.UnixMicro()
	if _, ok := r.next(np, now); !ok {
		t.Fatal("a new track should get its own end checks")
	}
}

func TestReconcilerRetriesMissingArtwork(t *testing.T) {
	now := time.Now()
	np := NowPlaying{Title: "Airbag"} // paused, no artwork yet
	var r reconciler

	for _, offset := range artChecks {
		at, ok := r.next(np, now)
		if !ok || !at.Equal(now.Add(offset)) {
			t.Fatalf("art check due %v (ok=%v), want %v", at.Sub(now), ok, offset)
		}
		r.fired(np, at)
	}
	if _, ok := r.next(np, now); ok {
		t.Fatal("art retries should give up")
	}

	np.ArtworkData = "art"
	if _, ok := r.next(np, now); ok {
		t.Fatal("nothing due once artwork has arrived")
	}
}
