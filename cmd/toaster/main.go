// Command toaster scores how ready a repository is for someone — a new hire or
// an agent — to ramp up on, and emits a cited, provenance-bearing scorecard.
//
// The binary is deterministic and pure: it reads a repo and prints a scorecard
// to stdout. Judgment, link resolution, and persistence belong to the optional
// skill layer that wraps it.
package main

import "github.com/tittle-xyz/toaster-ready/cmd"

func main() {
	cmd.Execute()
}
