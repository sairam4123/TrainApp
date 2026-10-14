package worlds

import "testing"

func TestWorldsBuild(t *testing.T) {
	for name, build := range map[string]func() any{
		"TpjKkdi": func() any { return BuildTpjKkdiWorld() },
		"Test":    func() any { return BuildTestWorld() },
		"SingleLine": func() any {
			return BuildSingleLineWorld(SingleLineOptions{TerminusPlatforms: 3, LoopPlatforms: 2, BlocksPerLine: 3})
		},
		"Junction": func() any { return BuildJunctionWorld(2) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("build panicked: %v", r)
				}
			}()
			build()
		})
	}
}
