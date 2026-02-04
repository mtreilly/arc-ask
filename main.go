// Copyright (c) 2025 Arc Engineering
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/yourorg/arc-ask/internal/cmd"
)

func main() {
	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "arc-ask: %v\n", err)
		os.Exit(1)
	}
}
