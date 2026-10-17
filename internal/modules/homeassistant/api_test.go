package homeassistant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLightStateUnavailable(t *testing.T) {
	cases := []struct {
		haState     string
		on          bool
		unavailable bool
	}{
		{"on", true, false},
		{"off", false, false},
		{"unavailable", false, true},
		{"unknown", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.haState, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"state": %q, "attributes": {}}`, tc.haState)
			}))
			defer srv.Close()

			state, err := NewClient(srv.URL, "token").GetLightState(context.Background(), "light.test")
			if err != nil {
				t.Fatal(err)
			}
			if state.On != tc.on || state.Unavailable != tc.unavailable {
				t.Errorf("got On=%v Unavailable=%v, want On=%v Unavailable=%v",
					state.On, state.Unavailable, tc.on, tc.unavailable)
			}
		})
	}
}
