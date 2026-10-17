package timetable

import "testing"

func TestIsHalt(t *testing.T) {
	cases := []struct {
		kind      string
		extra     float64
		waitedFor string
		want      bool
	}{
		{KindStop, 0.28, "U", true},  // a short wait for a train at a stop
		{KindStop, 0.28, "", false},  // for no train
		{KindStop, 0.28, "T", false}, // for itself
		{KindStop, 0.004, "U", false},
		{KindPass, 0.28, "U", false}, // a pass keeps the halt_min rule
		{KindPass, 0.6, "", true},
		{KindStop, 0.6, "", true},
	}
	for _, c := range cases {
		if got := isHalt(c.kind, c.extra, 0.5, c.waitedFor, "T"); got != c.want {
			t.Errorf("isHalt(%s, %v, for %q) = %v, want %v", c.kind, c.extra, c.waitedFor, got, c.want)
		}
	}
}
