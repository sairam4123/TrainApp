package edit

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
)

// Problem is something wrong with a world that keeps it from being run,
// though it can be drawn and saved: what Snapshot.Build reports, and track
// that ends nowhere. Piece is what it is about, if that is known.
type Problem struct {
	Piece schematic.Piece
	Text  string
}

// Problems lists what is wrong with the world, nil if it can be run.
func Problems(s *infra.Snapshot) []Problem {
	var out []Problem
	for _, p := range s.Points {
		w, e := tracksAt(s, p.ID)
		piece := schematic.Piece{Kind: schematic.PiecePoint, ID: p.ID}
		switch {
		case len(w)+len(e) == 0:
			out = append(out, Problem{piece, p.ID + ": a point with no track"})
		case p.DeadEnd:
		case len(w) == 0:
			out = append(out, Problem{piece, p.ID + ": open end, no track west (extend it, or make it a buffer stop or the world's edge)"})
		case len(e) == 0:
			out = append(out, Problem{piece, p.ID + ": open end, no track east (extend it, or make it a buffer stop or the world's edge)"})
		}
	}
	for _, st := range s.Stations {
		if len(st.Platforms) == 0 {
			out = append(out, Problem{schematic.Piece{Kind: schematic.PieceStation, ID: st.Code}, st.Code + ": a station with no platform"})
		}
	}
	if len(s.Tracks) == 0 {
		out = append(out, Problem{Text: "the world has no track"})
	}
	out = append(out, lineProblems(s)...)
	if _, err := s.Build(); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			out = append(out, Problem{about(s, line), line})
		}
	}
	slices.SortStableFunc(out, func(a, b Problem) int { return cmp.Compare(a.Text, b.Text) })
	return out
}

// about guesses the piece a message from Snapshot.Build is about: the one
// whose id it names, the longest if it names several.
func about(s *infra.Snapshot, msg string) schematic.Piece {
	var best schematic.Piece
	try := func(kind schematic.PieceKind, id string) {
		if id != "" && strings.Contains(msg, id) && len(id) > len(best.ID) {
			best = schematic.Piece{Kind: kind, ID: id}
		}
	}
	for _, p := range s.Points {
		try(schematic.PiecePoint, p.ID)
	}
	for _, t := range s.Tracks {
		try(schematic.PieceTrack, t.ID)
	}
	for _, sig := range s.Signals {
		try(schematic.PieceSignal, sig.ID)
	}
	for _, st := range s.Stations {
		try(schematic.PieceStation, st.Code)
	}
	return best
}

// Describe says what a piece is, in a line or two, for an inspector.
func Describe(s *infra.Snapshot, p schematic.Piece) []string {
	switch p.Kind {
	case schematic.PiecePoint:
		i := pointIndex(s, p.ID)
		if i < 0 {
			return nil
		}
		pt := s.Points[i]
		w, e := tracksAt(s, p.ID)
		end := "open"
		switch {
		case pt.Boundary:
			end = "the world's edge"
		case pt.DeadEnd:
			end = "a buffer stop"
		}
		code := cmp.Or(pt.Stn, "none (out on the line)")
		return []string{
			"point " + p.ID,
			fmt.Sprintf("station code %s · %d track(s) west, %d east · end: %s", code, len(w), len(e), end),
		}
	case schematic.PieceTrack:
		i := trackIndex(s, p.ID)
		if i < 0 {
			return nil
		}
		t := s.Tracks[i]
		kind := "track"
		if inSwitch(s, t.ID) {
			kind = "switch leg"
		}
		if code := platformOf(s, t.ID); code != "" {
			for _, st := range s.Stations {
				for _, pf := range st.Platforms {
					if pf.Track == t.ID {
						kind = fmt.Sprintf("%s platform %s (%g m)", code, pf.No, pf.LengthM)
						if pf.LengthM == 0 {
							kind = fmt.Sprintf("%s through track %s", code, pf.No)
						}
					}
				}
			}
		}
		line := fmt.Sprintf("%s · %g m · %g km/h", kind, t.LengthM, t.MaxKmph)
		if t.Lanes != 0 {
			line += fmt.Sprintf(" · %+d lanes", t.Lanes)
		}
		way := map[byte]string{WayEast: "runs east by its signals", WayWest: "runs west by its signals", WayBoth: "runs both ways by its signals", 0: "no signal at either end"}[Way(s, t.ID)]
		if t.Direction != "" {
			way = t.Direction + " line · " + way
		}
		return []string{"track " + trackName(t), line, t.From + " → " + t.To, way}
	case schematic.PieceSignal:
		i := signalIndex(s, p.ID)
		if i < 0 {
			return nil
		}
		sig := s.Signals[i]
		return []string{"signal " + sig.ID, "at " + sig.At + ", for trains from " + sig.Facing}
	case schematic.PieceStation:
		i := stationIndex(s, p.ID)
		if i < 0 {
			return nil
		}
		st := s.Stations[i]
		var pfs []string
		for _, pf := range st.Platforms {
			pfs = append(pfs, pf.No)
		}
		return []string{"station " + st.Code + " " + st.Name, "platforms " + cmp.Or(strings.Join(pfs, ", "), "none")}
	}
	return nil
}

// lineProblems are a line's signals that do not add up: a line whose way
// changes along it (from east to west, or from both ways to one: a signal
// missing in the middle, say), and an UP or DOWN label its signals do not
// bear out.
func lineProblems(s *infra.Snapshot) []Problem {
	var out []Problem
	wayName := map[byte]string{WayEast: "east", WayWest: "west", WayBoth: "both ways"}
	up := byte(WayWest)
	if s.Defaults.Up == "E" {
		up = WayEast
	}
	for _, t := range s.Tracks {
		piece := schematic.Piece{Kind: schematic.PieceTrack, ID: t.ID}
		way := Way(s, t.ID)
		if way != 0 {
			// the next signalled track east, over plain track only
			at := t.To
			for {
				w, e := tracksAt(s, at)
				if len(w) != 1 || len(e) != 1 || inSwitch(s, s.Tracks[e[0]].ID) {
					break
				}
				next := s.Tracks[e[0]]
				nw := Way(s, next.ID)
				if nw == 0 {
					at = next.To
					continue
				}
				if nw != way {
					out = append(out, Problem{schematic.Piece{Kind: schematic.PiecePoint, ID: at},
						fmt.Sprintf("%s: the line runs %s west of it and %s east of it", at, wayName[way], wayName[nw])})
				}
				break
			}
		}
		if t.Direction == string(infra.Up) || t.Direction == string(infra.Down) {
			want := up
			if t.Direction == string(infra.Down) {
				want = opposite(up)
			}
			if way != 0 && way != want {
				out = append(out, Problem{piece, fmt.Sprintf("%s: labelled %s, which runs %s, but signalled %s", trackName(t), t.Direction, wayName[want], wayName[way])})
			}
		}
	}
	return out
}

// opposite is the other one-way way.
func opposite(way byte) byte {
	if way == WayEast {
		return WayWest
	}
	return WayEast
}
