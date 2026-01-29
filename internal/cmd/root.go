// Copyright (c) 2025 Arc Engineering
// SPDX-License-Identifier: MIT

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/yourorg/arc-sdk/ai"
)

// NewRootCmd creates the root command for arc-ask.
func NewRootCmd(aiCfg *ai.Config) *cobra.Command {
	root := newAskCmd(aiCfg)
	return root
}
