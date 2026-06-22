// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"github.com/tittle-xyz/toaster-ready/internal/config"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

var configFile string

func init() {
	configCmd.Flags().StringVar(&configFile, "config", "", "path to a toaster config file (default: .toaster-ready.yml at the repo root)")
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config <path|owner/repo>",
	Short: "Print the resolved toaster config (defaults + any .toaster-ready.yml overrides)",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		r, err := repo.Open(args[0])
		if err != nil {
			return err
		}
		defer r.Close()

		cfg, path, err := config.Load(r.Root, configFile)
		if err != nil {
			return err
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"source":   sourceLabel(path),
			"resolved": cfg,
		})
	},
}

func sourceLabel(path string) string {
	if path == "" {
		return "built-in defaults"
	}
	return path
}
