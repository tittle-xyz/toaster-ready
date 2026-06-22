// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/tittle-xyz/toaster-ready/internal/check"
	"github.com/tittle-xyz/toaster-ready/internal/config"
	"github.com/tittle-xyz/toaster-ready/internal/githubclient"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

var (
	gateConfig string
	gateMin    float64
)

func init() {
	gateCmd.Flags().StringVar(&gateConfig, "config", "", "path to a toaster config file (default: .toaster-ready.yml at the repo root)")
	gateCmd.Flags().Float64Var(&gateMin, "min", -1, "minimum score (0-100) to pass; overrides the config gate threshold")
	rootCmd.AddCommand(gateCmd)
}

var gateCmd = &cobra.Command{
	Use:   "gate <path|owner/repo>",
	Short: "Fail (non-zero exit) if a repo misses the ramp-up floor or score threshold",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		r, err := repo.Open(args[0])
		if err != nil {
			return err
		}
		defer r.Close()

		cfg, _, err := config.Load(r.Root, gateConfig)
		if err != nil {
			return err
		}
		threshold := cfg.Gate.Threshold
		if gateMin >= 0 {
			threshold = gateMin
		}

		// Deterministic + no secrets (stub client) so the gate runs under GitHub
		// Free org-secret constraints; API-only signals surface as no-data.
		gh := githubclient.NewStub()
		scoredAt := time.Now().UTC().Format(time.RFC3339)
		sc := check.Run(r, gh, scoredAt, cfg)

		failures := check.GateFailures(sc, threshold)

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"repo": sc.Repo, "score": sc.Score, "max": sc.Max, "band": sc.Band,
			"gate": map[string]any{"threshold": threshold, "passed": len(failures) == 0, "failures": failures},
		})

		if len(failures) > 0 {
			fmt.Fprintf(os.Stderr, "toaster gate: FAILED (%d)\n", len(failures))
			r.Close() // deferred Close won't run after os.Exit; clean up the temp clone
			os.Exit(2)
		}
		return nil
	},
}
