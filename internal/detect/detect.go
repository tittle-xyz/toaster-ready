// SPDX-License-Identifier: Apache-2.0

// Package detect identifies a repository's language/stack so category checkers
// can apply the right signals. Per ADR-0002 (decision 5) detection is hybrid and
// generic-first: a marker-file table covers the common ecosystems, and
// language-specific detail layers in behind the same seam (see Profile) over time.
//
// detect is deliberately free of any scorecard dependency — it returns plain
// results, and the scoring layer converts a Stack's Marker into provenance
// evidence. That keeps detection reusable and trivially testable.
package detect

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

// Stack is one detected language/ecosystem and the file that identified it.
type Stack struct {
	ID      string `json:"id"`      // stable id: "go", "node", "python", ...
	Display string `json:"display"` // human label
	Marker  string `json:"marker"`  // relative path that identified the stack (the evidence)
}

// Result is the outcome of detection. An empty Stacks slice means the stack could
// not be determined — the caller treats that as no-data, never as a zero score.
type Result struct {
	Stacks []Stack `json:"stacks"`
}

// Undetermined reports whether no stack could be identified.
func (res Result) Undetermined() bool { return len(res.Stacks) == 0 }

// Has reports whether a stack id was detected.
func (res Result) Has(id string) bool {
	for _, s := range res.Stacks {
		if s.ID == id {
			return true
		}
	}
	return false
}

// IDs returns the detected stack ids, sorted.
func (res Result) IDs() []string {
	ids := make([]string, 0, len(res.Stacks))
	for _, s := range res.Stacks {
		ids = append(ids, s.ID)
	}
	return ids
}

// matcher maps marker files to a stack. A stack matches if any exact base name
// (names) or any glob against the base name (globs) is found anywhere in the tree.
type matcher struct {
	id      string
	display string
	names   []string
	globs   []string
}

// matchers is the generic-first marker table. It identifies the ecosystem; the
// per-language signal detail lives in Profile, not here.
var matchers = []matcher{
	{"go", "Go", []string{"go.mod"}, nil},
	{"node", "Node.js", []string{"package.json"}, nil},
	{"python", "Python", []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg", "Pipfile"}, nil},
	{"php", "PHP", []string{"composer.json"}, nil},
	{"ruby", "Ruby", []string{"Gemfile"}, []string{"*.gemspec"}},
	{"rust", "Rust", []string{"Cargo.toml"}, nil},
	{"java", "Java/JVM", []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle"}, nil},
	{"dotnet", ".NET", nil, []string{"*.csproj", "*.fsproj", "*.sln"}},
	{"elixir", "Elixir", []string{"mix.exs"}, nil},
	{"terraform", "Terraform/OpenTofu", nil, []string{"*.tf"}},
}

// Detect identifies the stacks present in r. A repo may be polyglot, so all
// matching stacks are returned. The shallowest marker wins as the evidence path,
// so a root manifest is preferred over a nested one.
func Detect(r *repo.Repo) Result {
	files := r.Files()
	sort.SliceStable(files, func(i, j int) bool {
		if di, dj := depth(files[i]), depth(files[j]); di != dj {
			return di < dj
		}
		return files[i] < files[j]
	})

	found := map[string]Stack{}
	for _, rel := range files {
		base := filepath.Base(rel)
		for _, m := range matchers {
			if _, seen := found[m.id]; seen {
				continue
			}
			if matchesName(base, m.names) || matchesGlob(base, m.globs) {
				found[m.id] = Stack{ID: m.id, Display: m.display, Marker: rel}
			}
		}
	}

	out := make([]Stack, 0, len(found))
	for _, s := range found {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return Result{Stacks: out}
}

// Profile carries language-specific detail behind the detection seam. Category
// checkers consult a detected stack's profile for targeted signals, and fall back
// to generic heuristics when a stack is unprofiled (ADR-0002 decision 5). It will
// grow as language-specific detectors are added; the seam stays stable.
type Profile struct {
	ID            string   `json:"id"`
	LockFiles     []string `json:"lockFiles,omitempty"`     // dependency lockfiles (repro + patching signal)
	CoverageGlobs []string `json:"coverageGlobs,omitempty"` // artifacts/paths indicating coverage reporting
}

var profiles = map[string]Profile{
	"go":     {ID: "go", LockFiles: []string{"go.sum"}, CoverageGlobs: []string{"coverage.out", "*.coverprofile"}},
	"node":   {ID: "node", LockFiles: []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml"}, CoverageGlobs: []string{"coverage/lcov.info", "coverage"}},
	"python": {ID: "python", LockFiles: []string{"poetry.lock", "Pipfile.lock"}, CoverageGlobs: []string{".coverage", "coverage.xml", "htmlcov"}},
	"php":    {ID: "php", LockFiles: []string{"composer.lock"}, CoverageGlobs: []string{"coverage.xml", "clover.xml", "coverage"}},
}

// ProfileFor returns the profile for a stack id, or a generic (empty) profile so
// callers can always ask without a nil check — the generic-first contract.
func ProfileFor(id string) Profile {
	if p, ok := profiles[id]; ok {
		return p
	}
	return Profile{ID: id}
}

func depth(rel string) int { return strings.Count(rel, string(filepath.Separator)) }

func matchesName(base string, names []string) bool {
	for _, n := range names {
		if base == n {
			return true
		}
	}
	return false
}

func matchesGlob(base string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := filepath.Match(g, base); ok {
			return true
		}
	}
	return false
}
