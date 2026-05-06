// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package version

import "fmt"

// Build metadata injected by CI via ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info describes the build metadata exposed by the binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// Current returns the current build metadata.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}

// String returns a human-readable summary.
func String() string {
	info := Current()
	return fmt.Sprintf("backupgate version=%s commit=%s build_date=%s", info.Version, info.Commit, info.BuildDate)
}
