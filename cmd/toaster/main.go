// Command toaster scores how ready a repository is for someone — a new hire or
// an agent — to ramp up on, and emits a cited, provenance-bearing scorecard.
//
// The binary is deterministic and pure: it reads a repo and prints a scorecard
// to stdout. Judgment, link resolution, and persistence belong to the optional
// skill layer that wraps it.
package main

import "github.com/tittle-xyz/toaster-ready/cmd"

// version is stamped at build time via -ldflags "-X main.version=...".
// A plain `go build` leaves it as "dev"; a release build carries the tag.
var version = "dev"

func main() {
	cmd.Execute(version)
}
