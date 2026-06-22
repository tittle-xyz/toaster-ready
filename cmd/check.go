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
	"github.com/tittle-xyz/toaster-ready/internal/render"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

var (
	checkOffline bool
	checkConfig  string
	checkFormat  string
)

func init() {
	checkCmd.Flags().BoolVar(&checkOffline, "offline", false, "skip GitHub API; report API signals as no-data")
	checkCmd.Flags().StringVar(&checkConfig, "config", "", "path to a toaster config file (default: .toaster-ready.yml at the repo root)")
	checkCmd.Flags().StringVar(&checkFormat, "format", "json", "output format: json | markdown | html")
	rootCmd.AddCommand(checkCmd)
}

var checkCmd = &cobra.Command{
	Use:   "check <path|owner/repo>",
	Short: "Score a repo and print the scorecard JSON to stdout",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		r, err := repo.Open(args[0])
		if err != nil {
			return err
		}
		defer r.Close()

		cfg, _, err := config.Load(r.Root, checkConfig)
		if err != nil {
			return err
		}

		gh := newGitHub(checkOffline)
		scoredAt := time.Now().UTC().Format(time.RFC3339)
		sc := check.Run(r, gh, scoredAt, cfg)

		switch checkFormat {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(sc)
		case "markdown", "md":
			fmt.Fprint(os.Stdout, render.Markdown(sc))
		case "html":
			fmt.Fprint(os.Stdout, render.HTML(sc))
		default:
			return fmt.Errorf("unknown format %q (want json, markdown, or html)", checkFormat)
		}
		return nil
	},
}

// newGitHub returns the live go-github backend, or the no-data stub when
// offline is requested or the repo slug is unknown (nothing to query).
func newGitHub(offline bool) githubclient.Client {
	if offline {
		return githubclient.NewStub()
	}
	c, _ := githubclient.New()
	return c
}
