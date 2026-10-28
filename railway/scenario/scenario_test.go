package scenario_test

import (
	"os"
	"path/filepath"
	"testing"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

// The repo's own scenarios load, and what they name exists.
func TestRepoScenarios(t *testing.T) {
	scs, err := scenario.LoadDir(filepath.Join("..", "..", "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scs {
		if _, err := worlds.Load(sc.WorldName()); err != nil {
			t.Errorf("%s: %v", sc.Name, err)
		}
		trains, err := ops.LoadTrains(sc.SchedulePath())
		if err != nil {
			t.Errorf("%s: %v", sc.Name, err)
			continue
		}
		if p := sc.ProfilePath(); p != "" {
			if _, err := sim.LoadPriorityConfig(p); err != nil {
				t.Errorf("%s: %v", sc.Name, err)
			}
		}
		if err := ops.ApplyDisruptions(trains, sc.Disruptions); err != nil {
			t.Errorf("%s: %v", sc.Name, err)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.json")
	if err := os.WriteFile(path, []byte(`{"world": "w.json", "schedule": "s.json", "profile": "/abs/p.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Name != "mine" || sc.WorldName() != filepath.Join(dir, "w.json") || sc.SchedulePath() != filepath.Join(dir, "s.json") || sc.ProfilePath() != "/abs/p.json" {
		t.Errorf("name %q, world %q, schedule %q, profile %q", sc.Name, sc.WorldName(), sc.SchedulePath(), sc.ProfilePath())
	}

	for name, body := range map[string]string{
		"no schedule":   `{"world": "tpjkkdi"}`,
		"unknown field": `{"world": "tpjkkdi", "schedule": "s.json", "trains": 3}`,
		"bad safety":    `{"world": "tpjkkdi", "schedule": "s.json", "safety": "no"}`,
	} {
		bad := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(bad, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.Load(bad); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestApplyRule(t *testing.T) {
	cfg := &sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{Rule: "thresholds"}}
	if err := scenario.ApplyRule(cfg, "weighted"); err != nil || cfg.Precedence.Rule != "weighted" {
		t.Errorf("rule %q, %v", cfg.Precedence.Rule, err)
	}
	if err := scenario.ApplyRule(cfg, scenario.RuleOff); err != nil || cfg.Precedence != nil {
		t.Errorf("precedence %+v after off, %v", cfg.Precedence, err)
	}
	if err := scenario.ApplyRule(cfg, "weighted"); err == nil {
		t.Error("a rule without a precedence section accepted")
	}
	if err := scenario.ApplyRule(nil, ""); err != nil {
		t.Error(err)
	}
}
