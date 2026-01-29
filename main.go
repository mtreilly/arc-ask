// Copyright (c) 2025 Arc Engineering
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/yourorg/arc-ask/internal/cmd"
	"github.com/yourorg/arc-sdk/ai"
)

func main() {
	aiCfg, err := ai.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "arc-ask: failed to load AI config: %v\n", err)
		os.Exit(1)
	}

	root := cmd.NewRootCmd(aiCfg)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
