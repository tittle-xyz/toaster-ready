// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"github.com/tittle-xyz/toaster-ready/internal/detect"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

func init() {
	rootCmd.AddCommand(detectCmd)
}

var detectCmd = &cobra.Command{
	Use:   "detect <path|owner/repo>",
	Short: "Detect a repository's language/stack (hybrid, generic-first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		r, err := repo.Open(args[0])
		if err != nil {
			return err
		}
		defer r.Close()

		res := detect.Detect(r)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"repo":         r.Slug,
			"ref":          r.Ref,
			"undetermined": res.Undetermined(),
			"stacks":       res.Stacks,
		})
	},
}
