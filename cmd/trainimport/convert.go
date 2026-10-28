package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/worlds/edit"
)

// A real schedule is train number to the train's calls, as the public
// timetable gives them:
//
//	{"16849": [{"index": "6", "code": "TPJ", "name": "TIRUCHCHIRAPALI", "dist": "0",
//	            "pf": "7", "a": "Source", "d": "07:05", "a_day": "1", "d_day": "1"}, ...]}
//
// a and d are HH:MM, or Source and Destination at a train's ends; the days
// are days of the train's journey. pf is blank where the timetable gives
// none, and dist is km along the train's whole journey.
type realSchedule map[string][]call

// call is a train's call at a station.
type call struct {
	Index string `json:"index"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Dist  string `json:"dist"`
	Pf    string `json:"pf"`
	A     string `json:"a"`
	D     string `json:"d"`
	ADay  string `json:"a_day"`
	DDay  string `json:"d_day"`
}

const (
	source      = "Source"
	destination = "Destination"
)

// trainList is what the real schedule leaves out about each train, by
// number:
//
//	{"trains": {"20497": {"name": "...", "class": "Superfast Express", "max_speed_kmph": 110,
//	                      "enter": {"stn": "KKDI", "at": "00:55"}}}}
//
// enter is when a train that comes into the world without calling passes
// the station it enters at, in place of the estimate (see extend).
type trainList struct {
	Trains map[string]trainInfo `json:"trains"`
}

type trainInfo struct {
	Name         string     `json:"name,omitempty"`
	Class        string     `json:"class"`
	MaxSpeedKmph float64    `json:"max_speed_kmph"`
	Stock        *ops.Stock `json:"stock,omitempty"`
	Enter        *enterAt   `json:"enter,omitempty"`
}

type enterAt struct {
	Stn string `json:"stn"`
	At  string `json:"at"`
}

// options are the converter's settings: the day starts at from (minutes
// after midnight), a train stands sourceMin at its Source before it leaves
// and destMin at its Destination after it arrives.
type options struct {
	from, sourceMin, destMin float64
}

func readJSON(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// convert makes the real schedule's trains a schedule file's, in the order
// they enter, along the world's track. classes are the profile's (nil
// skips the check). It reports everything wrong it finds, and warns of each
// train it runs on beyond the calls listed.
func convert(s *infra.Snapshot, sched realSchedule, list trainList, classes map[string]int, opt options) ([]ops.TrainSpec, []string, error) {
	var trains []ops.TrainSpec
	var warnings []string
	var errs []error
	for _, number := range slices.Sorted(maps.Keys(sched)) {
		info, ok := list.Trains[number]
		if !ok {
			errs = append(errs, fmt.Errorf("train %s: not in the train list", number))
			continue
		}
		if classes != nil {
			if _, ok := classes[info.Class]; !ok {
				errs = append(errs, fmt.Errorf("train %s: class %q is not one of the profile's", number, info.Class))
				continue
			}
		}
		t, warn, err := convertTrain(s, number, sched[number], info, opt)
		if err != nil {
			errs = append(errs, fmt.Errorf("train %s: %w", number, err))
			continue
		}
		trains = append(trains, t)
		warnings = append(warnings, warn...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, warnings, err
	}
	slices.SortStableFunc(trains, func(a, b ops.TrainSpec) int {
		return cmp.Or(cmp.Compare(*a.Points[0].Arr, *b.Points[0].Arr), strings.Compare(a.Number, b.Number))
	})
	return trains, warnings, nil
}

// stop is a call with its times in minutes, unwrapped across midnight.
type stop struct {
	call
	arr, dep float64
}

// convertTrain is one train: its calls timed (times), run on to where the
// world ends if it comes from or goes beyond it (extend), with a pass at
// each station between, then moved a day on if it enters before the day
// starts.
func convertTrain(s *infra.Snapshot, number string, calls []call, info trainInfo, opt options) (ops.TrainSpec, []string, error) {
	t := ops.TrainSpec{Number: number, Name: info.Name, Class: info.Class, MaxSpeedKmph: info.MaxSpeedKmph, Stock: info.Stock}
	stops, err := times(calls, opt)
	if err != nil {
		return t, nil, err
	}
	listed := make([]string, len(stops))
	for i, st := range stops {
		listed[i] = st.Code
	}
	route, err := routeThrough(s, listed)
	if err != nil {
		return t, nil, err
	}
	ext, warnings, err := extend(s, number, route.Stations, stops, info.Enter)
	if err != nil {
		return t, nil, err
	}

	at := map[string]stop{}
	for _, st := range stops {
		at[st.Code] = st
	}
	for i, stn := range ext.stations {
		st, ok := at[stn]
		switch {
		case ok:
			t.Points = append(t.Points, ops.PointSpec{Stn: stn, Kind: ops.SpStop, Pf: st.Pf, Arr: ptr(st.arr), Dep: ptr(st.dep)})
		case i == 0:
			t.Points = append(t.Points, ops.PointSpec{Stn: stn, Kind: ops.SpPass, Arr: ptr(ext.entry)})
		default:
			t.Points = append(t.Points, ops.PointSpec{Stn: stn, Kind: ops.SpPass})
		}
	}
	shift := 0.0
	for *t.Points[0].Arr+shift < opt.from {
		shift += 1440
	}
	for k := range t.Points {
		for _, v := range []*float64{t.Points[k].Arr, t.Points[k].Dep} {
			if v != nil {
				*v += shift
			}
		}
	}
	if err := check(s, t); err != nil {
		return t, warnings, err
	}
	return t, warnings, nil
}

// times are the calls' times in minutes: a time earlier than the one before
// it is the next day's. Source leaves sourceMin after the train comes in,
// and leaves its Destination destMin after it arrives. The journey days must agree.
func times(calls []call, opt options) ([]stop, error) {
	if len(calls) < 2 {
		return nil, fmt.Errorf("%d calls: a train needs two", len(calls))
	}
	var out []stop
	var errs []error
	var prev, off float64 = math.Inf(-1), 0
	firstDay := -1
	for k, c := range calls {
		st := stop{call: c, arr: math.NaN(), dep: math.NaN()}
		for _, v := range []struct {
			text, day, what string
			to              *float64
		}{{c.A, c.ADay, "a", &st.arr}, {c.D, c.DDay, "d", &st.dep}} {
			if v.text == source || v.text == destination {
				continue
			}
			clock, err := parseClock(v.text)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s %s: %w", c.Code, v.what, err))
				continue
			}
			t := clock + off
			for t < prev {
				off += 1440
				t += 1440
			}
			prev, *v.to = t, t
			if day, err := strconv.Atoi(v.day); err == nil {
				if firstDay < 0 {
					firstDay = day - int(t/1440)
				}
				if got := firstDay + int(t/1440); got != day {
					errs = append(errs, fmt.Errorf("%s %s: %s falls on day %d of its journey, the schedule says %d", c.Code, v.what, v.text, got, day))
				}
			}
		}
		switch {
		case c.A == source && k != 0, c.D == source:
			errs = append(errs, fmt.Errorf("%s: only the first call's a is Source", c.Code))
		case c.D == destination && k != len(calls)-1, c.A == destination:
			errs = append(errs, fmt.Errorf("%s: only the last call's d is Destination", c.Code))
		case c.A == source && !math.IsNaN(st.dep):
			st.arr = st.dep - opt.sourceMin
		case c.D == destination && !math.IsNaN(st.arr):
			st.dep = st.arr + opt.destMin
		}
		out = append(out, st)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for _, st := range out {
		if math.IsNaN(st.arr) || math.IsNaN(st.dep) {
			return nil, fmt.Errorf("%s: no time to call at", st.Code)
		}
	}
	return out, nil
}

// parseClock is HH:MM as minutes after midnight.
func parseClock(text string) (float64, error) {
	h, m, ok := strings.Cut(text, ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("%q is not a time (HH:MM)", text)
	}
	return float64(hh*60 + mm), nil
}

// clock is minutes as HH:MM on the clock.
func clock(t float64) string {
	m := int(math.Round(t))
	m = ((m % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// routeThrough is the shortest way over the track from the train's first
// station to its last that passes the others in order.
func routeThrough(s *infra.Snapshot, listed []string) (edit.Route, error) {
	first, last := listed[0], listed[len(listed)-1]
	if _, err := edit.FindRoute(s, first, last, ""); err != nil {
		return edit.Route{}, err
	}
	for _, r := range edit.Routes(s, first, last, 0) {
		if inOrder(r.Stations, listed) {
			return r, nil
		}
	}
	return edit.Route{}, fmt.Errorf("no way from %s to %s passes %s in that order", first, last, strings.Join(listed, " "))
}

// inOrder reports whether every one of want is in stations, in order.
func inOrder(stations, want []string) bool {
	i := 0
	for _, s := range stations {
		if i < len(want) && s == want[i] {
			i++
		}
	}
	return i == len(want)
}

// extended is the stations a train runs through, and when it enters at the
// first if that is not a call.
type extended struct {
	stations []string
	entry    float64
}

// extend runs a train on to where the world ends, if it comes from beyond
// its first call or goes on beyond its last: a first call that is not its
// Source (or a last that is not its Destination) at a station the track
// runs on beyond. It runs the longest way on, through each station there as
// a pass. It enters at the pass's time in the train list if given, else as
// long before its first call as its own average speed over its calls takes
// it to run the way in.
func extend(s *infra.Snapshot, number string, stations []string, stops []stop, enter *enterAt) (extended, []string, error) {
	first, last := stops[0], stops[len(stops)-1]
	out := extended{stations: stations, entry: first.arr}
	var warnings []string
	if first.A != source {
		if before := longest(s, func(x string) []edit.Route { return edit.Routes(s, x, stations[len(stations)-1], 0) }, stations, true); before != nil {
			in := before.Stations[:len(before.Stations)-len(stations)]
			out.stations = append(slices.Clone(in), stations...)
			entry, how, err := entryTime(s, in[0], first, last, enter)
			if err != nil {
				return out, nil, err
			}
			out.entry = entry
			warnings = append(warnings, fmt.Sprintf("%s: enters at %s, not %s where its calls start: passes %s at %s, %s",
				number, in[0], first.Code, in[0], clock(entry), how))
		}
	}
	if enter != nil && len(out.stations) == len(stations) {
		return out, nil, fmt.Errorf("the train list gives it an enter at %s, but it enters at %s, its first call", enter.Stn, first.Code)
	}
	if last.D != destination {
		head := out.stations
		if after := longest(s, func(x string) []edit.Route { return edit.Routes(s, head[0], x, 0) }, head, false); after != nil {
			on := after.Stations[len(head):]
			out.stations = append(slices.Clone(head), on...)
			warnings = append(warnings, fmt.Sprintf("%s: runs on to %s, beyond its last call at %s", number, on[len(on)-1], last.Code))
		}
	}
	return out, warnings, nil
}

// longest is the longest way, of those routes gives to or from a station,
// that runs over stations at its end (or, !before, its start) and on beyond
// them; nil if none does.
func longest(s *infra.Snapshot, routes func(string) []edit.Route, stations []string, before bool) *edit.Route {
	var best *edit.Route
	for _, st := range s.Stations {
		if slices.Contains(stations, st.Code) {
			continue
		}
		for _, r := range routes(st.Code) {
			n := len(r.Stations) - len(stations)
			if n <= 0 {
				continue
			}
			part := r.Stations[:len(stations)]
			if before {
				part = r.Stations[n:]
			}
			if slices.Equal(part, stations) && (best == nil || r.M > best.M) {
				best = &r
			}
		}
	}
	return best
}

// entryTime is when a train that enters at stn without calling passes it,
// before its first call, and how that was had.
func entryTime(s *infra.Snapshot, stn string, first, last stop, enter *enterAt) (float64, string, error) {
	if enter != nil {
		if enter.Stn != stn {
			return 0, "", fmt.Errorf("the train list has it enter at %s, but it enters at %s", enter.Stn, stn)
		}
		t, err := parseClock(enter.At)
		if err != nil {
			return 0, "", fmt.Errorf("enter: %w", err)
		}
		t += math.Floor(first.arr/1440) * 1440
		for t > first.arr {
			t -= 1440
		}
		return t, "as the train list gives", nil
	}
	d1, err1 := strconv.ParseFloat(first.Dist, 64)
	d2, err2 := strconv.ParseFloat(last.Dist, 64)
	km, mins := math.Abs(d2-d1), last.arr-first.dep
	if err1 != nil || err2 != nil || km <= 0 || mins <= 0 {
		return 0, "", fmt.Errorf("it enters at %s without calling, and its dist and times give no speed to run there at: give an enter in the train list", stn)
	}
	way, err := edit.FindRoute(s, stn, first.Code, "")
	if err != nil {
		return 0, "", err
	}
	run := way.M / 1000 / (km / mins)
	return math.Round(first.arr - run), fmt.Sprintf("%.0f min before %s, at its %.0f km in %.0f min from %s to %s (an estimate: an enter in the train list replaces it)",
		math.Round(run), first.Code, km, mins, first.Code, last.Code), nil
}

// check is what trainapp would refuse in a train: the schedule file's rules,
// and stations, platforms and a way the world does not have.
func check(s *infra.Snapshot, t ops.TrainSpec) error {
	var errs []error
	if _, err := t.Train(); err != nil {
		errs = append(errs, err)
	}
	pfs := map[string][]string{}
	for _, st := range s.Stations {
		pfs[st.Code] = []string{}
		for _, p := range st.Platforms {
			pfs[st.Code] = append(pfs[st.Code], p.No)
		}
	}
	for _, p := range t.Points {
		no, ok := pfs[p.Stn]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("the world has no station %s", p.Stn))
		case p.Pf != "" && !slices.Contains(no, p.Pf):
			errs = append(errs, fmt.Errorf("%s has no platform %s (it has %s)", p.Stn, p.Pf, strings.Join(no, ", ")))
		}
	}
	if len(errs) == 0 {
		if _, err := edit.TrainPath(s, t.Points); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func ptr(v float64) *float64 { return &v }
