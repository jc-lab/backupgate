// Copyright 2025 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

//go:build tools
// +build tools

package tools

import (
	_ "github.com/google/addlicense"
)

//go:generate go run github.com/google/addlicense -c "JC-Lab" -l "AGPL-3.0-only" -s .
