package infra_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"
)

var snapshotWorlds = map[string]func() *infra.World{
	"TpjKkdi":           worlds.BuildTpjKkdiWorld,
	"TpjKkdiSingleLine": worlds.BuildTpjKkdiSingleLineWorld,
	"Test":              worlds.BuildTestWorld,
	"SingleLine": func() *infra.World {
		return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 3, LoopPlatforms: 2, BlocksPerLine: 3, ThroughTrack: true})
	},
	"Junction": func() *infra.World { return worlds.BuildJunctionWorld(2) },
}

// throughJSON snapshots the world, writes it as JSON and builds it again.
func throughJSON(t *testing.T, w *infra.World) (*infra.Snapshot, *infra.World) {
	t.Helper()
	snap, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var back infra.Snapshot
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	w2, err := back.Build()
	if err != nil {
		t.Fatal(err)
	}
	return snap, w2
}

func TestSnapshotRoundTrip(t *testing.T) {
	for name, build := range snapshotWorlds {
		t.Run(name, func(t *testing.T) {
			w := build()
			snap, w2 := throughJSON(t, w)
			snap2, err := w2.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snap, snap2) {
				t.Error("the rebuilt world's snapshot differs")
			}
			if !reflect.DeepEqual(vizproto.FromWorld(w), vizproto.FromWorld(w2)) {
				t.Error("the rebuilt world draws differently (or its station tracks differ)")
			}

			incident := map[ids.PointID][]*infra.GraphEdge{}
			for _, trk := range w.TrackGraph.ListTracks() {
				e := w.TrackGraph.Edges[trk.Id]
				e2, ok := w2.TrackGraph.Edges[trk.Id]
				if !ok {
					t.Errorf("track %s is missing", trk.Id)
					continue
				}
				// exactly, not through the snapshot's km/h
				if e2.Track.MaxSpeed != trk.MaxSpeed || e2.Track.Length != trk.Length || e2.Lanes != e.Lanes {
					t.Errorf("track %s: speed %v length %v lanes %d, want %v %v %d", trk.Id,
						e2.Track.MaxSpeed, e2.Track.Length, e2.Lanes, trk.MaxSpeed, trk.Length, e.Lanes)
				}
				if got, want := switchIDs(w2.SwitchBlocksOn(e2.Track)), switchIDs(w.SwitchBlocksOn(trk)); !slices.Equal(got, want) {
					t.Errorf("track %s: switch blocks %v, want %v in that order", trk.Id, got, want)
				}
				incident[e.From.Id] = append(incident[e.From.Id], e)
				incident[e.To.Id] = append(incident[e.To.Id], e)
			}
			for id, edges := range incident {
				pt, _ := w.TrackGraph.GetPoint(id)
				pt2, _ := w2.TrackGraph.GetPoint(id)
				for _, a := range edges {
					for _, b := range edges {
						a2, b2 := w2.TrackGraph.Edges[a.Track.Id], w2.TrackGraph.Edges[b.Track.Id]
						if got, want := w2.TrackGraph.TransitionAllowed(pt2, a2, b2), w.TrackGraph.TransitionAllowed(pt, a, b); got != want {
							t.Errorf("at %s from %s to %s: allowed %v, want %v", id, a.Track.Id, b.Track.Id, got, want)
						}
					}
				}
			}
		})
	}
}

func switchIDs(sbs []*infra.SwitchBlock) []string {
	var out []string
	for _, sb := range sbs {
		out = append(out, sb.Id)
	}
	return out
}

func TestSaveAndLoadWorld(t *testing.T) {
	w := worlds.BuildTpjKkdiSingleLineWorld()
	path := filepath.Join(t.TempDir(), "world.json")
	if err := infra.SaveWorld(path, w); err != nil {
		t.Fatal(err)
	}
	w2, err := infra.LoadWorld(path)
	if err != nil {
		t.Fatal(err)
	}
	if !w2.IsFinalized() {
		t.Error("a loaded world is not finalized")
	}
	if !reflect.DeepEqual(vizproto.FromWorld(w), vizproto.FromWorld(w2)) {
		t.Error("the loaded world differs")
	}
}

// tinySnapshot is a station A with one platform, the track off it to the
// world's edge, and a signal; edit it to break it.
func tinySnapshot() *infra.Snapshot {
	return &infra.Snapshot{
		Version: infra.SnapshotVersion,
		Defaults: infra.SnapshotDefaults{
			SwitchKmph: 30, SwitchLengthM: 100, PlatformKmph: 50, PlatformLengthM: 800, RouteKmph: 110,
		},
		Points: []infra.SnapshotPoint{
			{Stn: "A", Type: "Pf", Seq: "1", Other: "S", DeadEnd: true, Boundary: true},
			{ID: "aPf1E", Stn: "A", Type: "Pf", Seq: "1", Other: "E"},
			{Stn: "A", Type: "Pf", Seq: "2", Other: "S"},
			{Stn: "A", Type: "Pf", Seq: "2", Other: "E"},
			{Stn: "L", Type: "Bk", Seq: "1", DeadEnd: true, Boundary: true},
		},
		Tracks: []infra.SnapshotTrack{
			{From: "aPf1S", To: "aPf1E", LengthM: 800, MaxKmph: 50},
			{From: "aPf2S", To: "aPf2E", LengthM: 800, MaxKmph: 50},
			{From: "aPf1E", To: "lBk1", LengthM: 5000, MaxKmph: 110},
			{From: "aPf2E", To: "lBk1", LengthM: 5000, MaxKmph: 110, Lanes: -1},
		},
		Switches: []infra.SnapshotSwitch{
			{ID: "sw", Tracks: []string{"aPf1E-lBk1", "aPf2E-lBk1"}, Default: "aPf1E-lBk1"},
		},
		Signals: []infra.SnapshotSignal{{ID: "sig", At: "lBk1", Facing: "aPf1E"}},
		Stations: []infra.SnapshotStation{{Code: "A", Name: "Alpha", Platforms: []infra.SnapshotPlatform{
			{No: "1", Track: "aPf1E-aPf1S", LengthM: 600},
			{No: "2", Track: "aPf2E-aPf2S", LengthM: 600},
		}}},
	}
}

func TestSnapshotBuild(t *testing.T) {
	w, err := tinySnapshot().Build()
	if err != nil {
		t.Fatal(err)
	}
	stn, ok := w.GetStation("A")
	if !ok || len(stn.Platforms()) != 2 {
		t.Fatal("station A with two platforms is missing")
	}
	pf1, pf2 := w.TrackGraph.Edges[stn.FindStnPlatform("1").Id], w.TrackGraph.Edges[stn.FindStnPlatform("2").Id]
	if pf1 == nil || pf2 == nil {
		t.Fatal("the platform tracks are missing")
	}
	leg1, _ := w.TrackGraph.TrackBetween(pf1.To, &infra.TrackPoint{Id: ids.PointID{StnCode: "L", Type: "Bk", Seq: "1"}})
	leg2, _ := w.TrackGraph.TrackBetween(pf2.To, &infra.TrackPoint{Id: ids.PointID{StnCode: "L", Type: "Bk", Seq: "1"}})
	toe, _ := w.TrackGraph.GetPoint(ids.PointID{StnCode: "L", Type: "Bk", Seq: "1"})
	if w.TrackGraph.TransitionAllowed(toe, leg1, leg2) {
		t.Error("a train can run from one switch leg onto the other")
	}
}

func TestSnapshotBuildErrors(t *testing.T) {
	for _, c := range []struct {
		name  string
		edit  func(s *infra.Snapshot)
		wants []string
	}{
		{"version", func(s *infra.Snapshot) { s.Version = 99 }, []string{"version 99"}},
		{"point id doesn't match its parts", func(s *infra.Snapshot) { s.Points[1].ID = "aPf9E" }, []string{`id "aPf9E", but its parts make "aPf1E"`}},
		{"point twice", func(s *infra.Snapshot) { s.Points = append(s.Points, s.Points[0]) }, []string{"point aPf1S: listed twice"}},
		{"unknown point and bad speed", func(s *infra.Snapshot) {
			s.Tracks[0].To = "nope"
			s.Tracks[1].MaxKmph = 0
		}, []string{`no point "nope"`, "max_kmph must be positive"}},
		{"track twice", func(s *infra.Snapshot) { s.Tracks = append(s.Tracks, s.Tracks[0]) }, []string{"listed twice"}},
		{"track id doesn't match its points", func(s *infra.Snapshot) { s.Tracks[0].ID = "x-y" }, []string{`id "x-y", but its points make "aPf1E-aPf1S"`}},
		{"default not a switch track", func(s *infra.Snapshot) { s.Switches[0].Default = "aPf1E-aPf1S" }, []string{"default aPf1E-aPf1S is not one of its tracks"}},
		{"unknown switch track", func(s *infra.Snapshot) { s.Switches[0].Tracks[1] = "zz" }, []string{`switch sw: no track "zz"`}},
		{"signal off the track", func(s *infra.Snapshot) { s.Signals[0].Facing = "aPf2S" }, []string{"signal sig: no track between"}},
		{"platform twice", func(s *infra.Snapshot) { s.Stations[0].Platforms[1].No = "1" }, []string{"station A platform 1: listed twice"}},
		{"unknown platform track", func(s *infra.Snapshot) { s.Stations[0].Platforms[0].Track = "q" }, []string{`station A platform 1: no track "q"`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := tinySnapshot()
			c.edit(s)
			_, err := s.Build()
			if err == nil {
				t.Fatal("built without an error")
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
		})
	}
}
