package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/phinze/belowdeck/internal/device"
)

// The init-failure streak only advances when the Stream Deck's HID endpoint is
// wedged, which is rare and impossible to provoke on demand. Cover it here so
// the logic does not rot between incidents.

func TestInitFailureStreakAccumulates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearInitFailures()

	for want := 1; want <= 4; want++ {
		if got := noteInitFailure(); got != want {
			t.Fatalf("noteInitFailure() = %d, want %d", got, want)
		}
	}
}

func TestSuccessfulInitClearsStreak(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearInitFailures()

	noteInitFailure()
	noteInitFailure()
	clearInitFailures()

	if got := noteInitFailure(); got != 1 {
		t.Fatalf("after clear, noteInitFailure() = %d, want 1", got)
	}
}

func TestStaleStreakStartsOver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearInitFailures()

	// A streak from well outside the window is unrelated history, not a
	// continuing failure.
	stale := time.Now().Add(-2 * initFailureWindow).Unix()
	if err := os.MkdirAll(filepath.Dir(initFailurePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initFailurePath(),
		[]byte(fmt.Sprintf("%d %d", 7, stale)), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := noteInitFailure(); got != 1 {
		t.Fatalf("stale streak continued: got %d, want 1", got)
	}
}

func TestInitBackoffStaysQuietThenRamps(t *testing.T) {
	// Below the loud threshold we exit immediately, so a wedge a respawn can
	// actually clear still recovers in seconds.
	for f := 1; f < initFailureLoud; f++ {
		if d := initBackoff(f); d != 0 {
			t.Fatalf("initBackoff(%d) = %s, want 0", f, d)
		}
	}
	if d := initBackoff(initFailureLoud); d <= 0 {
		t.Fatalf("initBackoff(%d) = %s, want > 0", initFailureLoud, d)
	}
	// And it must not grow without bound.
	if d := initBackoff(1000); d != initBackoffMax {
		t.Fatalf("initBackoff(1000) = %s, want %s", d, initBackoffMax)
	}
	if a, b := initBackoff(initFailureLoud), initBackoff(initFailureLoud+1); b <= a {
		t.Fatalf("backoff did not increase: %s then %s", a, b)
	}
}

// fakeInitTarget scripts the brightness write: each call pops the next error
// from the queue, and once the queue is empty the write succeeds.
type fakeInitTarget struct {
	brightnessErrs []error
	brightnessSet  []byte
	clearErr       error
	cleared        int
}

func (f *fakeInitTarget) SetBrightness(perc byte) error {
	if len(f.brightnessErrs) > 0 {
		err := f.brightnessErrs[0]
		f.brightnessErrs = f.brightnessErrs[1:]
		return err
	}
	f.brightnessSet = append(f.brightnessSet, perc)
	return nil
}

func (f *fakeInitTarget) ForEachKey(cb func(device.KeyID) error) error {
	for k := device.KeyID(1); k <= 8; k++ {
		if err := cb(k); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeInitTarget) ClearKey(device.KeyID) error {
	if f.clearErr != nil {
		return f.clearErr
	}
	f.cleared++
	return nil
}

func TestInitDeviceRetriesTransientBrightnessFailure(t *testing.T) {
	notResponding := errors.New("device not responding")
	dev := &fakeInitTarget{brightnessErrs: []error{notResponding}}

	if err := initDevice(dev); err != nil {
		t.Fatalf("initDevice() = %v, want nil after one transient failure", err)
	}
	if len(dev.brightnessSet) != 1 || dev.brightnessSet[0] != 80 {
		t.Fatalf("brightness writes = %v, want [80]", dev.brightnessSet)
	}
	if dev.cleared != 8 {
		t.Fatalf("cleared %d keys, want 8", dev.cleared)
	}
}

func TestInitDeviceReportsPersistentBrightnessFailure(t *testing.T) {
	notResponding := errors.New("device not responding")
	errs := make([]error, initBrightnessAttempts)
	for i := range errs {
		errs[i] = notResponding
	}
	dev := &fakeInitTarget{brightnessErrs: errs}

	err := initDevice(dev)
	if !errors.Is(err, notResponding) {
		t.Fatalf("initDevice() = %v, want wrapped %v", err, notResponding)
	}
	// A dark panel is the failure we care about; do not go on to clear keys
	// and pretend the deck is usable.
	if dev.cleared != 0 {
		t.Fatalf("cleared %d keys after brightness failed, want 0", dev.cleared)
	}
}

func TestInitDeviceReportsClearFailure(t *testing.T) {
	clearErr := errors.New("set output report failed")
	dev := &fakeInitTarget{clearErr: clearErr}

	if err := initDevice(dev); !errors.Is(err, clearErr) {
		t.Fatalf("initDevice() = %v, want wrapped %v", err, clearErr)
	}
}

func TestBackoffEndsEarlyOnFreshArrival(t *testing.T) {
	arrived := make(chan struct{}, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		arrived <- struct{}{}
	}()

	start := time.Now()
	if !backoffUntilArrival(5*time.Second, arrived) {
		t.Fatal("backoffUntilArrival() = false, want true after arrival")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("backoff took %s, want it cut short by the arrival", time.Since(start))
	}
}

func TestBackoffIgnoresStaleArrival(t *testing.T) {
	// The startup probe leaves one edge buffered; it predates the failure
	// and must not end the backoff.
	arrived := make(chan struct{}, 1)
	arrived <- struct{}{}

	if backoffUntilArrival(100*time.Millisecond, arrived) {
		t.Fatal("backoffUntilArrival() = true on a stale arrival, want false")
	}
}
