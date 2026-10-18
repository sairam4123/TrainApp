// Package trainapp holds the demo's scenarios and profiles, embedded so
// traindemo runs from any directory.
package trainapp

import "embed"

// Demo is the scenarios and profiles directories, laid out as in the repo so
// the scenarios' relative paths still resolve.
//
//go:embed scenarios profiles
var Demo embed.FS
