// SPDX-License-Identifier: Apache-2.0

// Package ctxbudget estimates the always-loaded agent-context footprint of a
// repository — the instructions and memory that get injected into an agent every
// session — so bloat can be penalized (a facet of the agent-instructions category
// in ADR-0002).
//
// The key distinction is always-loaded vs lazy-loaded. A lean root instruction
// file that points at skills/imports loaded on demand is good; a monolith
// injected every session is the bloat to catch. Only the always-loaded set is
// counted: root instruction files, an auto-loaded memory index, and one level of
// @path imports those files reference. Skills and on-demand memory are excluded
// by design.
//
// The token estimate is a deterministic char/token heuristic — no tokenizer, no
// agent — keeping it CI-safe.
package ctxbudget

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

// Budget thresholds in always-loaded tokens. Sane defaults; config will make
// them overridable. Over soft = getting heavy; over hard = "can't even load it all."
const (
	SoftBudgetTokens = 6000
	HardBudgetTokens = 16000
)

// Status classifies an always-loaded footprint against the budget.
type Status string

const (
	Within   Status = "within"
	OverSoft Status = "over-soft"
	OverHard Status = "over-hard"
)

// FileEstimate is one always-loaded file's contribution.
type FileEstimate struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Tokens int    `json:"tokens"`
	Kind   string `json:"kind"` // instructions | memory-index | import
}

// Estimate is the always-loaded footprint of a repo's agent context.
type Estimate struct {
	AlwaysLoadedTokens int            `json:"alwaysLoadedTokens"`
	Files              []FileEstimate `json:"files"`
}

// Status classifies the footprint against the default budget.
func (e Estimate) Status() Status { return e.Classify(SoftBudgetTokens, HardBudgetTokens) }

// Classify classifies the footprint against caller-supplied thresholds.
func (e Estimate) Classify(soft, hard int) Status {
	switch {
	case e.AlwaysLoadedTokens > hard:
		return OverHard
	case e.AlwaysLoadedTokens > soft:
		return OverSoft
	default:
		return Within
	}
}

// TokensFromBytes is the deterministic estimate (~4 chars/token).
func TokensFromBytes(n int) int { return (n + 3) / 4 }

var instructionNames = []string{
	"CLAUDE.md", ".claude/CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md",
}

var memoryIndexNames = []string{
	"MEMORY.md", ".claude/memory/MEMORY.md", "memory/MEMORY.md",
}

// importRe matches always-loaded @path imports (e.g. "@./docs/conventions.md").
// It requires a path-like token (starts with . or /) so it doesn't match
// @mentions or email-style strings.
var importRe = regexp.MustCompile(`(?m)(?:^|\s)@([./][^\s]+)`)

// Compute estimates r's always-loaded agent-context footprint.
func Compute(r *repo.Repo) Estimate {
	var est Estimate
	seen := map[string]bool{}

	add := func(path, kind string) {
		if path == "" || seen[path] {
			return
		}
		body, err := r.Read(path)
		if err != nil {
			return
		}
		seen[path] = true
		toks := TokensFromBytes(len(body))
		est.Files = append(est.Files, FileEstimate{Path: path, Bytes: len(body), Tokens: toks, Kind: kind})
		est.AlwaysLoadedTokens += toks
	}

	var instr []string
	for _, name := range instructionNames {
		if r.Exists(name) {
			instr = append(instr, name)
			add(name, "instructions")
		}
	}
	for _, name := range memoryIndexNames {
		if r.Exists(name) {
			add(name, "memory-index")
		}
	}
	// Resolve one level of @path imports referenced by the instruction files.
	for _, p := range instr {
		body, _ := r.Read(p)
		for _, imp := range findImports(body) {
			if r.Exists(imp) {
				add(imp, "import")
			}
		}
	}
	return est
}

func findImports(body string) []string {
	var out []string
	for _, m := range importRe.FindAllStringSubmatch(body, -1) {
		p := strings.TrimRight(strings.TrimSpace(m[1]), "`)\"',;:")
		p = strings.TrimPrefix(p, "/") // treat absolute as repo-root-relative
		// Reject traversal: filepath.Clean keeps leading "..", and repo.Read also
		// guards, but drop it here so it never reaches the filesystem layer.
		if p = filepath.Clean(p); p != "" && p != "." && !strings.HasPrefix(p, "..") {
			out = append(out, p)
		}
	}
	return out
}
