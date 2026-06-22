// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "toaster",
	Short: "Score how ready a repo is to ramp up on (human or agent)",
	Long: `toaster-ready scores a repository against the ramp-up readiness rubric and emits a
cited JSON scorecard. It is deterministic and pure — it reads a repo and prints
a scorecard; judgment, link resolution, and persistence belong to the skill
layer that wraps it.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "toaster:", err)
		os.Exit(1)
	}
}
