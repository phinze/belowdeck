package homeassistant

import (
	"image"
	"testing"
)

func TestUnavailableBadge(t *testing.T) {
	m := &Module{}
	if err := m.initFonts(); err != nil {
		t.Fatal(err)
	}

	// A pixel inside the badge disc but off the "!" glyph.
	hasBadge := func(img image.Image) bool {
		return img.At(badgeCX-badgeRadius+2, badgeCY) == colorAlertRed
	}

	for _, tc := range []struct {
		name  string
		state LightState
		want  bool
	}{
		{"on", LightState{On: true}, false},
		{"off", LightState{}, false},
		{"unavailable", LightState{Unavailable: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.officeLightState = tc.state
			m.ringLightState = tc.state
			if got := hasBadge(m.renderOfficeTimeButton()); got != tc.want {
				t.Errorf("office key badge = %v, want %v", got, tc.want)
			}
			if got := hasBadge(m.renderRingLightButton()); got != tc.want {
				t.Errorf("ring key badge = %v, want %v", got, tc.want)
			}
		})
	}
}
