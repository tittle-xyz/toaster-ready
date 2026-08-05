// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	checkOut     string
)

func init() {
	checkCmd.Flags().BoolVar(&checkOffline, "offline", false, "skip GitHub API; report API signals as no-data")
	checkCmd.Flags().StringVar(&checkConfig, "config", "", "path to a toaster config file (default: .toaster-ready.yml at the repo root)")
	checkCmd.Flags().StringVar(&checkFormat, "format", "json", "output format: json | markdown | html | shields | svg")
	checkCmd.Flags().StringVar(&checkOut, "out", "", "write to this file instead of stdout; leaves the file untouched when the content is unchanged")
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
		defer func() { _ = r.Close() }()

		cfg, _, err := config.Load(r.Root, checkConfig)
		if err != nil {
			return err
		}

		gh := newGitHub(checkOffline)
		scoredAt := time.Now().UTC().Format(time.RFC3339)
		sc := check.Run(r, gh, scoredAt, cfg)

		var buf bytes.Buffer
		switch checkFormat {
		case "json":
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			if err := enc.Encode(sc); err != nil {
				return err
			}
		case "markdown", "md":
			fmt.Fprint(&buf, render.Markdown(sc))
		case "html":
			fmt.Fprint(&buf, render.HTML(sc))
		case "shields":
			fmt.Fprintln(&buf, render.Shields(sc))
		case "svg":
			fmt.Fprint(&buf, render.BadgeSVG(sc))
		default:
			return fmt.Errorf("unknown format %q (want json, markdown, html, shields, or svg)", checkFormat)
		}

		if checkOut == "" {
			_, err = os.Stdout.Write(buf.Bytes())
			return err
		}
		return writeIfChanged(checkOut, buf.Bytes())
	},
}

// writeIfChanged writes b to path only when it differs from what is already
// there, so re-running is a genuine no-op — the file keeps its mtime and git
// sees nothing.
//
// That matters for the pre-commit hook: a hook that rewrites the badge on every
// commit makes pre-commit report "files were modified", forcing a re-stage for a
// change that is not there, and turns a shared generated file into a source of
// pointless merge conflicts. Only the `svg` and `shields` formats are stable
// enough for this — `json` embeds a scoredAt timestamp and so always differs.
func writeIfChanged(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, b, 0o644)
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
