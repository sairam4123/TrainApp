package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// BenchRun is one scenario run under one rule, as trainbench writes them to
// its -out file and traindemo to its baseline.
type BenchRun struct {
	Scenario string  `json:"scenario"`
	Rule     string  `json:"rule"`
	WallSec  float64 `json:"wall_sec"`
	Err      string  `json:"error,omitempty"`
	Report   *Report `json:"report,omitempty"`
}

// WriteBench writes runs as a bench file.
func WriteBench(w io.Writer, runs []BenchRun) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(runs)
}

// ReadBench reads a bench file.
func ReadBench(path string) ([]BenchRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var runs []BenchRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return runs, nil
}
