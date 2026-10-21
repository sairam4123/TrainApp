package worlds

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"trainapp/railway/infra"
)

var update = flag.Bool("update", false, "rewrite the golden world snapshots in testdata/")

// Every world builds to the snapshot in testdata/, so moving the builders
// about (e.g. into worlds/templates) cannot change a world unnoticed. Run
// with -update to take a deliberate change.
func TestSnapshotsUnchanged(t *testing.T) {
	for name, build := range map[string]func() *infra.World{
		"tpjkkdi":    BuildTpjKkdiWorld,
		"tpjkkdi-sl": BuildTpjKkdiSingleLineWorld,
		"tpjkkdi-dl": BuildTpjKkdiDoubleLineWorld,
		"single": func() *infra.World {
			return BuildSingleLineWorld(SingleLineOptions{TerminusPlatforms: 3, LoopPlatforms: 2, BlocksPerLine: 3})
		},
		"junction": func() *infra.World { return BuildJunctionWorld(2) },
	} {
		t.Run(name, func(t *testing.T) {
			s, err := build().Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".json")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test ./worlds -run TestSnapshotsUnchanged -update)", err)
			}
			if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
				t.Errorf("%s no longer builds to %s", name, path)
			}
		})
	}
}
