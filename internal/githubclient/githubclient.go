// SPDX-License-Identifier: Apache-2.0

// Package githubclient is the narrow seam between toaster-ready's checkers and GitHub.
//
// Keeping this an interface means dimensions 4 (CI status) and 5 (branch
// protection) are testable without a network, and the backend (go-github,
// added in Phase B) is swappable without touching callers. Every method returns
// a Result whose Status may be no-data — e.g. a 403 on branch protection for a
// non-admin token — which the checkers surface as a reason rather than a 0.
package githubclient

// Result is the three-state outcome of an API lookup, mirroring scorecard's
// no-data discipline at the source.
type Result struct {
	OK     bool   // the determination, when Available
	NoData bool   // true if the fact could not be retrieved
	Reason string // why, when NoData
	Detail string // human note (e.g. "conclusion=success")
}

// NoData builds a no-data Result.
func NoData(reason string) Result { return Result{NoData: true, Reason: reason} }

// Client is the minimal surface toaster-ready needs from GitHub.
type Client interface {
	// LatestRunGreen reports whether the most recent CI run on the default
	// branch concluded successfully.
	LatestRunGreen(slug string) Result
	// BranchProtected reports whether the default branch has protection rules.
	BranchProtected(slug string) Result
}

// Stub is the Phase-A implementation: it makes no network calls and reports
// every fact as no-data. This lets the full pipeline — including the no-data
// rendering — run before the go-github backend lands.
type Stub struct {
	Reason string
}

// NewStub returns a Stub with a default reason.
func NewStub() Stub {
	return Stub{Reason: "github backend not configured (phase-a stub)"}
}

func (s Stub) LatestRunGreen(string) Result  { return NoData(s.Reason) }
func (s Stub) BranchProtected(string) Result { return NoData(s.Reason) }
