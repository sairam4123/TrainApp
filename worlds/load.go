package worlds

import (
	"fmt"
	"path/filepath"
	"trainapp/railway/infra"
)

// Builtin names the worlds Load builds by name.
var Builtin = []string{"tpjkkdi", "tpjkkdi-sl", "tpjkkdi-dl"}

// Load builds a world by name: tpjkkdi (the simplified double line),
// tpjkkdi-sl (the real single line), tpjkkdi-dl (the real line doubled) or a
// world snapshot file (.json).
func Load(name string) (*infra.World, error) {
	switch name {
	case "tpjkkdi":
		return BuildTpjKkdiWorld(), nil
	case "tpjkkdi-sl":
		return BuildTpjKkdiSingleLineWorld(), nil
	case "tpjkkdi-dl":
		return BuildTpjKkdiDoubleLineWorld(), nil
	}
	if filepath.Ext(name) != ".json" {
		return nil, fmt.Errorf("unknown world %q: want tpjkkdi, tpjkkdi-sl, tpjkkdi-dl or a world snapshot (.json)", name)
	}
	return infra.LoadWorld(name)
}
