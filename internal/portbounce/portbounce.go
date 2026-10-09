// Package portbounce power-cycles the USB hub port a device is plugged into,
// the software equivalent of unplugging it and plugging it back in.
//
// It drives uhubctl, which only works when the device sits behind a hub that
// supports per-port power switching (the CalDigit TS4's internal hubs do, as
// do the common VIA Labs chipsets). Anything else reports ErrUnavailable so
// the caller can fall back to asking a human.
package portbounce

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrUnavailable means a bounce cannot be attempted here: uhubctl is not
// installed, or the device is not behind a hub uhubctl can switch.
var ErrUnavailable = errors.New("port bounce unavailable")

// Location is a device's place in the uhubctl hub tree.
type Location struct {
	Hub  string
	Port string
}

func (l Location) String() string { return fmt.Sprintf("hub %s port %s", l.Hub, l.Port) }

var (
	hubRe  = regexp.MustCompile(`^Current status for hub (\S+) \[[^\]]*\]`)
	portRe = regexp.MustCompile(`^\s+Port (\d+):`)
)

// Find locates the port holding vid:pid in uhubctl's status output. Only hubs
// that advertise per-port power switching ("ppps") count; uhubctl refuses to
// switch the others without forcing, and forcing is not something to do
// unattended.
func Find(out string, vid, pid uint16) (Location, error) {
	want := fmt.Sprintf("[%04x:%04x", vid, pid)
	var hub string
	var switchable bool
	for line := range strings.SplitSeq(out, "\n") {
		if m := hubRe.FindStringSubmatch(line); m != nil {
			hub = m[1]
			switchable = strings.Contains(m[0], "ppps")
			continue
		}
		m := portRe.FindStringSubmatch(line)
		if m == nil || hub == "" || !strings.Contains(line, want) {
			continue
		}
		if !switchable {
			return Location{}, fmt.Errorf("%w: %04x:%04x is on hub %s, which cannot switch port power",
				ErrUnavailable, vid, pid, hub)
		}
		return Location{Hub: hub, Port: m[1]}, nil
	}
	return Location{}, fmt.Errorf("%w: %04x:%04x not found behind any hub uhubctl can see",
		ErrUnavailable, vid, pid)
}

// Bounce finds vid:pid and power-cycles its port, holding power off for
// offFor. It returns once power is back on; the device re-enumerates on its
// own schedule after that.
func Bounce(ctx context.Context, vid, pid uint16, offFor time.Duration) (Location, error) {
	uhubctl, err := exec.LookPath("uhubctl")
	if err != nil {
		return Location{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	// uhubctl can exit non-zero while still printing a usable tree (for
	// instance when some hub on the machine refuses to answer), so judge the
	// listing by its output.
	out, err := exec.CommandContext(ctx, uhubctl).CombinedOutput()
	if len(out) == 0 && err != nil {
		return Location{}, fmt.Errorf("uhubctl: %w", err)
	}
	loc, err := Find(string(out), vid, pid)
	if err != nil {
		return Location{}, err
	}

	delay := fmt.Sprintf("%d", max(1, int(offFor.Round(time.Second)/time.Second)))
	out, err = exec.CommandContext(ctx, uhubctl,
		"-l", loc.Hub, "-p", loc.Port, "-a", "cycle", "-d", delay).CombinedOutput()
	if err != nil {
		return loc, fmt.Errorf("uhubctl cycle %s: %w: %s", loc, err, strings.TrimSpace(string(out)))
	}
	return loc, nil
}
