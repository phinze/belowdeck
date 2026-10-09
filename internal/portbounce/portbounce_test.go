package portbounce

import (
	"errors"
	"testing"
)

// Trimmed from real uhubctl output on the CalDigit TS4 dock the deck lives
// on. The Cam Link shares Elgato's vendor ID, which is why matching has to
// include the product ID.
const tree = `Current status for hub 0-1.1 [2188:5510 CalDigit, Inc TS4 USB2.0 HUB, USB 2.01, 5 ports, ppps]
  Port 1: 0503 power highspeed enable connect [2188:5511 CalDigit, Inc TS4 USB2.0 HUB, USB 2.01, 4 ports, ppps]
  Port 2: 0503 power highspeed enable connect [2188:5512 CalDigit, Inc TS4 USB2.0 HUB, USB 2.01, 5 ports, ppps]
  Port 3: 0503 power highspeed enable connect [0fd9:0084]
  Port 4: 0100 power
  Port 5: 0103 power enable connect [2188:7112]
Current status for hub 0-2.1.4 [2109:0813 VIA Labs, Inc. USB3.0 Hub, USB 3.00, 4 ports, ppps]
  Port 1: 02a0 power 5gbps Rx.Detect
  Port 4: 0203 power 5gbps U0 enable connect [0fd9:007b Elgato Cam Link 4K A29YB327208CLW]
`

func TestFindStreamDeck(t *testing.T) {
	loc, err := Find(tree, 0x0fd9, 0x0084)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if want := (Location{Hub: "0-1.1", Port: "3"}); loc != want {
		t.Fatalf("Find() = %+v, want %+v", loc, want)
	}
}

func TestFindDoesNotConfuseSameVendor(t *testing.T) {
	loc, err := Find(tree, 0x0fd9, 0x007b)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if want := (Location{Hub: "0-2.1.4", Port: "4"}); loc != want {
		t.Fatalf("Find() = %+v, want %+v", loc, want)
	}
}

func TestFindMissingDevice(t *testing.T) {
	if _, err := Find(tree, 0x0fd9, 0x0060); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Find() error = %v, want ErrUnavailable", err)
	}
}

func TestFindRefusesHubWithoutPowerSwitching(t *testing.T) {
	ganged := `Current status for hub 0-1.5.3 [09ea:0130 Generic Virtual HUB 00000001, USB 1.01, 7 ports, ganged]
  Port 2: 0103 power enable connect [0fd9:0084]
`
	if _, err := Find(ganged, 0x0fd9, 0x0084); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Find() error = %v, want ErrUnavailable", err)
	}
}
