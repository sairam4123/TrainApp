package worlds

import (
	"fmt"
	"path/filepath"
	"trainapp/railway/infra"
)

// Load builds a world by name: tpjkkdi (the double line), tpjkkdi-sl (the
// real single line) or a world snapshot file (.json).
func Load(name string) (*infra.World, error) {
	switch name {
	case "tpjkkdi":
		return BuildTpjKkdiWorld(), nil
	case "tpjkkdi-sl":
		return BuildTpjKkdiSingleLineWorld(), nil
	}
	if filepath.Ext(name) != ".json" {
		return nil, fmt.Errorf("unknown world %q: want tpjkkdi, tpjkkdi-sl or a world snapshot (.json)", name)
	}
	return infra.LoadWorld(name)
}
