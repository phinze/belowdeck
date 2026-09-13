package hidcheck

import (
	"testing"

	"rafaelmartins.com/p/usbhid"
)

func TestCountAttributesOpenDevicesToThisProcess(t *testing.T) {
	before, err := Count()
	if err != nil {
		t.Fatal(err)
	}
	if before.Ours != 0 {
		t.Fatalf("expected 0 user clients before opening anything, got %d", before.Ours)
	}
	t.Logf("before: total=%d ours=%d", before.Total, before.Ours)

	// Open whichever HID device the sandbox lets an unentitled process see
	// (on a MacBook that is usually the keyboard backlight). One open must
	// show up as exactly one user client attributed to this pid.
	devs, err := usbhid.Enumerate(nil)
	if err != nil {
		t.Fatal(err)
	}
	var dev *usbhid.Device
	for _, d := range devs {
		if err := d.Open(false); err == nil {
			dev = d
			break
		}
	}
	if dev == nil {
		t.Skip("no HID device could be opened from this process")
	}

	during, err := Count()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("during: total=%d ours=%d", during.Total, during.Ours)
	if during.Ours != 1 {
		t.Errorf("expected 1 user client while device is open, got %d", during.Ours)
	}

	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}

	after, err := Count()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after: total=%d ours=%d", after.Total, after.Ours)
	if after.Ours != 0 {
		t.Errorf("expected 0 user clients after close, got %d", after.Ours)
	}
}
