package ops_test

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/units"
)

func f(v float64) *float64 { return &v }
func n(v int) *int         { return &v }

// locoHauled is the profile's placeholder: a 6120 hp locomotive and 20
// coaches.
var locoHauled = ops.Stock{
	HP: f(6120), Efficiency: f(0.85), LocoLengthM: f(21),
	Coaches: n(20), CoachLengthM: f(23.5), CoachTonnes: f(55), BrakeMS2: f(0.5),
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConsistFromMakeup(t *testing.T) {
	c, err := locoHauled.Consist(units.KMPH(160))
	if err != nil {
		t.Fatal(err)
	}
	if c.Length != 491 {
		t.Errorf("length %v, want 491 (21 + 20 × 23.5)", c.Length)
	}
	if hptl := 6120.0 / 1100; !near(c.HPPerTL, hptl) {
		t.Errorf("HP/TL %v, want %v", c.HPPerTL, hptl)
	}
	// 2 × 0.85 × 0.7457 × HP/TL / (160 km/h in m/s)
	want := 2 * 0.85 * 0.7457 * (6120.0 / 1100) / (160.0 / 3.6)
	if !near(c.Accel.InMS2(), want) {
		t.Errorf("accel %v m/s², want %v", c.Accel.InMS2(), want)
	}
	if math.Abs(c.Accel.InMS2()-0.159) > 0.001 {
		t.Errorf("accel %v m/s², want about 0.159", c.Accel.InMS2())
	}
	if !near(c.Brake.InMS2(), 0.5) {
		t.Errorf("brake %v m/s², want 0.5", c.Brake.InMS2())
	}
}

func TestConsistOverrides(t *testing.T) {
	// a 24-coach rake: longer, and a lower HP/TL
	c, err := locoHauled.With(&ops.Stock{Coaches: n(24)}).Consist(units.KMPH(160))
	if err != nil {
		t.Fatal(err)
	}
	if c.Length != 585 {
		t.Errorf("length %v, want 585", c.Length)
	}
	if !near(c.HPPerTL, 6120.0/1320) {
		t.Errorf("HP/TL %v, want %v", c.HPPerTL, 6120.0/1320)
	}

	// values set directly win over the makeup
	c, err = locoHauled.With(&ops.Stock{LengthM: f(400), AccelMS2: f(0.3)}).Consist(units.KMPH(160))
	if err != nil {
		t.Fatal(err)
	}
	if c.Length != 400 || !near(c.Accel.InMS2(), 0.3) {
		t.Errorf("length %v, accel %v m/s²; want 400 and 0.3", c.Length, c.Accel.InMS2())
	}

	// hp_per_tl set directly still goes through efficiency and the reference
	c, err = ops.Stock{HPPerTL: f(5), Efficiency: f(1)}.Consist(units.KMPH(160))
	if err != nil {
		t.Fatal(err)
	}
	if want := 2 * 0.7457 * 5 / (160.0 / 3.6); !near(c.Accel.InMS2(), want) || c.Length != 0 {
		t.Errorf("accel %v m/s², length %v; want %v and 0", c.Accel.InMS2(), c.Length, want)
	}

	if got := locoHauled.With(nil); got != locoHauled {
		t.Error("With(nil) changed the stock")
	}
}

func TestConsistWithoutLocomotive(t *testing.T) {
	vb := ops.Stock{HP: f(6120), Efficiency: f(0.85), LocoLengthM: f(0), Coaches: n(16), CoachLengthM: f(24), CoachTonnes: f(55)}
	c, err := vb.Consist(units.KMPH(160))
	if err != nil {
		t.Fatal(err)
	}
	if c.Length != 384 || !near(c.HPPerTL, 6120.0/880) {
		t.Errorf("length %v, HP/TL %v; want 384 and %v", c.Length, c.HPPerTL, 6120.0/880)
	}
	if c.Brake != 0 {
		t.Errorf("brake %v, want 0 (not given)", c.Brake)
	}
}

func TestConsistUnsetAndPartial(t *testing.T) {
	c, err := ops.Stock{}.Consist(0)
	if err != nil || c != (ops.Consist{}) {
		t.Errorf("empty stock: %+v, %v; want a zero consist", c, err)
	}

	noEfficiency := locoHauled
	noEfficiency.Efficiency = nil
	for _, tc := range []struct {
		name  string
		stock ops.Stock
		ref   units.MetersPerMin
		want  []string
	}{
		{"length", ops.Stock{LocoLengthM: f(21)}, units.KMPH(160), []string{"length needs coaches, coach_length_m"}},
		{"HP/TL", ops.Stock{HP: f(6120), Coaches: n(20), Efficiency: f(0.85)}, units.KMPH(160),
			[]string{"HP/TL needs coach_tonnes", "length needs loco_length_m, coach_length_m"}},
		{"efficiency", noEfficiency, units.KMPH(160), []string{"acceleration needs efficiency"}},
		{"reference", locoHauled, 0, []string{"positive reference speed"}},
	} {
		_, err := tc.stock.Consist(tc.ref)
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not say %q", tc.name, err, w)
			}
		}
	}
}

func TestStockValidate(t *testing.T) {
	if err := locoHauled.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := ops.Stock{HP: f(-1), Efficiency: f(1.2), LocoLengthM: f(-3), Coaches: n(0), BrakeMS2: f(0), AccelMS2: f(-0.1)}
	err := bad.Validate()
	if err == nil {
		t.Fatal("no error")
	}
	for _, w := range []string{"hp", "efficiency", "loco_length_m", "coaches", "brake_ms2", "accel_ms2"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not mention %s", err, w)
		}
	}
	if err := (&ops.RollingStock{Classes: map[string]ops.Stock{"X": locoHauled}}).Validate(); err == nil || !strings.Contains(err.Error(), "reference_kmph") {
		t.Errorf("rolling stock without a reference speed: %v", err)
	}
}

func TestConsistOfClassAndTrain(t *testing.T) {
	rs := &ops.RollingStock{ReferenceKmph: 160, Classes: map[string]ops.Stock{"Express": locoHauled}}
	tr := &ops.Train{Class: "Express", Stock: &ops.Stock{Coaches: n(24)}}
	c, err := rs.ConsistOf(tr)
	if err != nil || c.Length != 585 {
		t.Errorf("class + train: %+v, %v; want length 585", c, err)
	}
	c, err = rs.ConsistOf(&ops.Train{Class: "Unlisted"})
	if err != nil || c != (ops.Consist{}) {
		t.Errorf("unlisted class: %+v, %v; want a zero consist", c, err)
	}
	var none *ops.RollingStock
	c, err = none.ConsistOf(&ops.Train{Stock: &ops.Stock{LengthM: f(300)}})
	if err != nil || c.Length != 300 {
		t.Errorf("no profile, train's own: %+v, %v; want length 300", c, err)
	}
}

func TestScheduleStockReadsBack(t *testing.T) {
	const sched = `{"trains": [
  {"number": "1U", "class": "Express", "max_speed_kmph": 110, "stock": {"coaches": 24, "loco_length_m": 0},
   "points": [{"stn": "A", "kind": "stop", "dwell": 5, "arr": 1}]}]}`
	trains, err := ops.ReadTrains(strings.NewReader(sched))
	if err != nil {
		t.Fatal(err)
	}
	st := trains[0].Stock
	if st == nil || *st.Coaches != 24 || *st.LocoLengthM != 0 {
		t.Fatalf("stock %+v, want coaches 24 and loco_length_m 0", st)
	}
	var buf bytes.Buffer
	if err := ops.WriteTrains(&buf, trains); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"loco_length_m": 0`) {
		t.Errorf("written schedule lost loco_length_m 0:\n%s", buf.String())
	}
	if c := trains[0].Clone(); c.Stock != st {
		t.Error("Clone dropped the stock")
	}

	for _, bad := range []string{`{"coaches": 0}`, `{"carriages": 3}`} {
		s := strings.Replace(sched, `{"coaches": 24, "loco_length_m": 0}`, bad, 1)
		if _, err := ops.ReadTrains(strings.NewReader(s)); err == nil {
			t.Errorf("stock %s: no error", bad)
		}
	}
}
