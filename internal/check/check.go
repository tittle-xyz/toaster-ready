// SPDX-License-Identifier: Apache-2.0

// Package check runs the deterministic rubric (v2 — docs/adr/0002) against a
// repo and assembles a weighted /100 scorecard. Each category checker sets a
// normalized subscore in [0,1] from what presence/shape alone justify;
// categories marked Judgment leave room for a later (optional, off-CI) pass to
// refine the subscore. scorecard.Aggregate does the weighting + redistribution.
package check

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tittle-xyz/toaster-ready/internal/config"
	"github.com/tittle-xyz/toaster-ready/internal/ctxbudget"
	"github.com/tittle-xyz/toaster-ready/internal/detect"
	"github.com/tittle-xyz/toaster-ready/internal/githubclient"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

// RubricVersion is bumped whenever categories or scoring change.
const RubricVersion = "2.2"

// Signal names for the agent-instructions facets. recommend() keys on these so
// a bloated/stale/drifted-but-present file gets "fix it" advice rather than
// being treated as a missing file.
const (
	budgetSignal = "always-loaded context budget"
	staleSignal  = "instructions freshness"
	driftSignal  = "instructions command drift"
)

// Staleness tunables: instructions are flagged stale when at least
// staleMinCommits source commits postdate their last update (a floor that keeps
// tiny/young repos quiet) AND those make up at least staleRatio of all source
// history (so "the code moved on without the docs" is what trips it).
const (
	staleMinCommits = 8
	staleRatio      = 0.5
)

// Drift penalties on the agent-instructions subscore. A concretely broken
// command reference is a stronger signal than the staleness heuristic, so it
// costs more. Both are multiplicative and compose with the budget penalty.
const (
	driftMultStale = 0.8
	driftMultCmd   = 0.6
)

// Run scores r against cfg and returns a complete Scorecard. Pass
// config.Default() for the built-in rubric.
func Run(r *repo.Repo, gh githubclient.Client, scoredAt string, cfg config.Config) scorecard.Scorecard {
	stacks := mergeLanguageHints(detect.Detect(r), cfg.Languages)

	cats := []scorecard.Category{
		agentInstructions(r, cfg.ContextBudget),
		setupReproducibility(r),
		testingAndCoverage(r, stacks),
		cicdPipeline(r, gh),
		configSecrets(r),
		purposeOrientation(r),
		conventionsStandards(r, gh),
		sourceMaterialTrail(r),
		inRepoTooling(r),
		dependencyPatching(r, stacks),
		dbMigrations(r),
	}

	// Apply config: drop disabled categories, override weights.
	var kept []scorecard.Category
	for _, c := range cats {
		if cfg.IsDisabled(c.ID) {
			continue
		}
		if w, ok := cfg.Weights[c.ID]; ok {
			c.Weight = w
		}
		kept = append(kept, c)
	}

	score, complete := scorecard.Aggregate(kept)

	// Attach recommendations to categories scoring below the configured level.
	for i := range kept {
		if c := &kept[i]; c.Applicable && c.Normalized < cfg.Recommend.Below {
			c.Recommendations = recommend(*c)
		}
	}

	slug := r.Slug
	if slug == "" {
		slug = r.Root
	}
	return scorecard.Scorecard{
		Repo:          slug,
		Ref:           r.Ref,
		ScoredAt:      scoredAt,
		RubricVersion: RubricVersion,
		Scorer:        "toaster-ready " + RubricVersion + " (deterministic)",
		Score:         score,
		Max:           100,
		Band:          scorecard.Band(score),
		DataComplete:  complete,
		DetectedStack: stacks.IDs(),
		Categories:    kept,
	}
}

// gateCriticalCats are categories whose determined absence fails the gate's
// essentials floor — a repo missing these isn't ready for anyone to ramp onto.
var gateCriticalCats = map[string]bool{
	scorecard.CatAgentInstructions: true,
	scorecard.CatPurpose:           true,
	scorecard.CatCICD:              true,
}

// GateFailures returns the reasons a repo fails the gate: any critical category
// determined-absent (a real miss, never no-data), a hardcoded secret, or a total
// score below threshold. An empty slice means the gate passes.
func GateFailures(sc scorecard.Scorecard, threshold float64) []string {
	var failures []string
	for _, c := range sc.Categories {
		if gateCriticalCats[c.ID] && c.ScoredAbsent() {
			failures = append(failures, c.ID+": absent")
		}
		if c.ID == scorecard.CatConfigSecrets && hasHardcodedSecret(c) {
			failures = append(failures, "config-and-secrets: hardcoded secrets detected")
		}
	}
	if sc.Score < threshold {
		failures = append(failures, fmt.Sprintf("score %.1f below threshold %.0f", sc.Score, threshold))
	}
	return failures
}

func hasHardcodedSecret(c scorecard.Category) bool {
	for _, e := range c.Signals {
		if e.Signal == "hardcoded secret" && e.Status == scorecard.StatusOK && e.Found != nil && *e.Found {
			return true
		}
	}
	return false
}

// categoryAdvice is the templated "what good looks like" guidance per category,
// used for miss/improve recommendations. Deterministic — no agent.
var categoryAdvice = map[string]string{
	scorecard.CatAgentInstructions:  "Add a CLAUDE.md/AGENTS.md that documents how to build, test, and deploy; keep the always-loaded footprint lean.",
	scorecard.CatSetup:              "Document a single clone→running path (a task runner — Makefile/Taskfile or package.json scripts — a container, or a README setup section).",
	scorecard.CatTesting:            "Add tests and report coverage (a coverage step in CI or a codecov/coveralls config).",
	scorecard.CatCICD:               "Add a CI workflow that runs tests/build on push, and keep the latest run green.",
	scorecard.CatConfigSecrets:      "Provide a .env.example, gitignore .env, and remove any hardcoded secrets from source.",
	scorecard.CatPurpose:            "Add a README that answers what/why/who, with a few headings.",
	scorecard.CatConventions:        "Add linters/formatters, CODEOWNERS, semver tags, and branch protection.",
	scorecard.CatSourceTrail:        "Record decisions as ADRs and link the source material (Confluence/Jira) that explains the why.",
	scorecard.CatInRepoTooling:      "Add a task runner (Makefile/Taskfile), a scripts/ directory, or agent skills so common tasks are reproducible.",
	scorecard.CatDependencyPatching: "Pin dependencies with a lockfile and enable Dependabot or Renovate.",
	scorecard.CatDBMigrations:       "Manage schema changes with a migrations tool and a migrations/ directory.",
}

// recommend turns a below-threshold category into actionable advice, keyed off
// its failing signals and separating a real miss from a no-data gap.
func recommend(c scorecard.Category) []scorecard.Recommendation {
	advice := categoryAdvice[c.ID]
	var recs []scorecard.Recommendation
	var missRef string
	sawMiss, sawNoData, overBudget := false, false, false
	stale, cmdDrift := false, false
	var ndSignal, ndReason string

	for _, e := range c.Signals {
		switch {
		case e.Signal == budgetSignal && e.Status == scorecard.StatusOK && e.Found != nil && !*e.Found:
			// The file exists but its always-loaded footprint is over budget — a
			// "trim it" case, not an "add it" miss.
			overBudget = true
		case e.Signal == staleSignal && e.Status == scorecard.StatusOK && e.Found != nil && !*e.Found:
			// Present but stale vs source churn — "refresh it", not "add it".
			stale = true
		case e.Signal == driftSignal && e.Status == scorecard.StatusOK && e.Found != nil && !*e.Found:
			// Present but documents commands that don't exist — "fix it".
			cmdDrift = true
		case e.Status == scorecard.StatusOK && e.Found != nil && !*e.Found:
			if !sawMiss {
				sawMiss, missRef = true, evidenceRef(e)
			}
		case e.Status == scorecard.StatusNoData:
			if !sawNoData {
				sawNoData, ndSignal, ndReason = true, e.Signal, e.Reason
			}
		}
	}

	switch {
	case sawMiss:
		recs = append(recs, scorecard.Recommendation{Category: c.ID, Cause: scorecard.CauseMiss, Action: advice, EvidenceRef: missRef})
	case overBudget:
		recs = append(recs, scorecard.Recommendation{Category: c.ID, Cause: scorecard.CauseImprove, EvidenceRef: budgetSignal,
			Action: "Trim the always-loaded agent context (instructions + memory); move detail into lazy-loaded skills/imports to get under budget."})
	case cmdDrift || stale:
		// Specific drift recommendations are appended below; suppress generic advice.
	case !sawNoData:
		// Present but partial, with no explicit absent signal — strengthen it.
		recs = append(recs, scorecard.Recommendation{Category: c.ID, Cause: scorecard.CauseImprove, Action: advice})
	}
	if cmdDrift {
		recs = append(recs, scorecard.Recommendation{Category: c.ID, Cause: scorecard.CauseImprove, EvidenceRef: driftSignal,
			Action: "Fix or remove documented commands that don't exist in the repo (drifted make/npm/just targets); the instructions should run as written."})
	}
	if stale {
		recs = append(recs, scorecard.Recommendation{Category: c.ID, Cause: scorecard.CauseImprove, EvidenceRef: staleSignal,
			Action: "The instructions predate most of the source history — review them for drift and refresh the parts the code has moved past."})
	}
	if sawNoData {
		recs = append(recs, scorecard.Recommendation{
			Category: c.ID, Cause: scorecard.CauseNoData, EvidenceRef: ndSignal,
			Action: fmt.Sprintf("Couldn't determine %q (%s); make it checkable.", ndSignal, ndReason),
		})
	}
	return recs
}

func evidenceRef(e scorecard.Evidence) string {
	if e.Path != "" {
		return e.Signal + " @ " + e.Path
	}
	return e.Signal
}

// mergeLanguageHints augments detected stacks with config-supplied hints so a
// repo whose stack toaster-ready can't auto-detect still gets the right per-stack signals.
func mergeLanguageHints(res detect.Result, hints []string) detect.Result {
	for _, h := range hints {
		if h == "" || res.Has(h) {
			continue
		}
		res.Stacks = append(res.Stacks, detect.Stack{ID: h, Display: h, Marker: "(config)"})
	}
	return res
}

// newCategory seeds a category from the default rubric catalog. Applicable by
// default; checkers flip that off for not-applicable cases. Normalized starts 0.
func newCategory(id string) scorecard.Category {
	return scorecard.Category{
		ID: id, Title: scorecard.TitleFor(id), Weight: scorecard.WeightFor(id),
		Applicable: true, Judgment: true,
	}
}

// scaled converts a 0..2 deterministic raw score into a [0,1] normalized value,
// preserving the v0.1 three-step calibration while moving onto the /100 scale.
func scaled(raw int) float64 { return float64(raw) / 2 }

// --- agent instructions ----------------------------------------------------

// Agent/human instructions — can an agent act correctly without asking?
// (Facets: exists · explains the mechanics · fits a context budget.)
func agentInstructions(r *repo.Repo, cb config.ContextBudget) scorecard.Category {
	c := newCategory(scorecard.CatAgentInstructions)
	f := r.FirstExisting("CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md")
	if f == "" {
		c.Signals = append(c.Signals, evAbsent("agent instructions file", scorecard.MethodFile, ""))
		c.Normalized, c.Rationale = 0, "No CLAUDE.md/AGENTS.md; agents start cold."
		return c
	}
	body, _ := r.Read(f)
	c.Signals = append(c.Signals,
		evFound("agent instructions file", scorecard.MethodFile, f),
		evNote("instructions size", scorecard.MethodContent, f, fmt.Sprintf("%d bytes", len(body))),
	)
	// Facet 1+2 (exists / proxy for explains-the-mechanics): presence + substance.
	base := 0.5
	if len(body) >= 800 {
		base = 1.0
	}

	// Facet 3 (fits a context budget): the always-loaded footprint of instructions
	// + memory + imports. Bloat that an agent can't realistically load is penalized.
	est := ctxbudget.Compute(r)
	status := est.Classify(cb.Soft, cb.Hard)
	budgetMult, budgetNote := budgetPenalty(status)
	c.Signals = append(c.Signals, scorecard.Evidence{
		Signal: budgetSignal, Method: scorecard.MethodContent,
		Status: scorecard.StatusOK, Found: scorecard.Boolp(status == ctxbudget.Within),
		Source: "filesystem",
		Note:   fmt.Sprintf("%d tokens always-loaded across %d file(s) — %s budget", est.AlwaysLoadedTokens, len(est.Files), status),
		Metrics: map[string]any{
			"alwaysLoadedTokens": est.AlwaysLoadedTokens,
			"files":              est.Files,
			"softBudget":         cb.Soft,
			"hardBudget":         cb.Hard,
		},
	})

	// Facet 4 (stays true to the code): presence ≠ accuracy. We can't judge prose
	// correctness deterministically, but we can catch mechanical drift — see
	// instructionsDrift. It appends its own signals (incl. no-data when git
	// history is unavailable) and returns a multiplier on the subscore.
	driftMult := instructionsDrift(r, f, body, &c)

	c.Normalized = base * budgetMult * driftMult
	c.Rationale = "Facets: exists · explains the mechanics (length proxy) · fits a context budget · stays true to the code (freshness + command drift). " + budgetNote
	return c
}

// instructionsDrift evaluates whether present instructions have drifted from the
// code — the "presence ≠ accuracy" gap. It appends two signals to c and returns
// a multiplier in (0,1]:
//
//   - freshness (git): how many source commits postdate the last instructions
//     edit. Three-state — no-data on a shallow/non-git tree, never a penalty.
//   - command drift (content): documented make/npm/just targets that don't
//     resolve to a real manifest entry — a concretely wrong instruction.
//
// Found follows the codebase convention "the good property holds": Found=true
// means fresh / no drift. recommend() special-cases both signals so a stale or
// drifted (but present) file isn't reported as a missing file.
func instructionsDrift(r *repo.Repo, instrPath, body string, c *scorecard.Category) float64 {
	mult := 1.0

	if since, total, ok := r.InstructionsChurn(instrPath); !ok {
		c.Signals = append(c.Signals, evNoData(staleSignal, scorecard.MethodGit,
			"no git history (not a git repo, a shallow clone, or the file is untracked)"))
	} else {
		ratio := 0.0
		if total > 0 {
			ratio = float64(since) / float64(total)
		}
		stale := since >= staleMinCommits && ratio >= staleRatio
		c.Signals = append(c.Signals, scorecard.Evidence{
			Signal: staleSignal, Method: scorecard.MethodGit, Status: scorecard.StatusOK,
			Found: scorecard.Boolp(!stale), Path: instrPath, Source: "filesystem",
			Note: fmt.Sprintf("%d of %d source commits postdate the last instructions update (%.0f%%)", since, total, ratio*100),
			Metrics: map[string]any{
				"sourceCommitsSince": since, "sourceCommitsTotal": total, "ratio": ratio,
			},
		})
		if stale {
			mult *= driftMultStale
		}
	}

	hits := commandDrift(r, body)
	drift := scorecard.Evidence{
		Signal: driftSignal, Method: scorecard.MethodContent, Status: scorecard.StatusOK,
		Found: scorecard.Boolp(len(hits) == 0), Path: instrPath, Source: "filesystem",
	}
	if len(hits) > 0 {
		drift.Note = "documented commands not found in the repo: " + strings.Join(hits, ", ")
		drift.Metrics = map[string]any{"missingCommands": hits}
		mult *= driftMultCmd
	} else {
		drift.Note = "documented make/npm/just targets resolve to real manifest entries"
	}
	c.Signals = append(c.Signals, drift)

	return mult
}

// budgetPenalty maps a context-budget status to a multiplier on the
// agent-instructions subscore and a one-line rationale fragment.
func budgetPenalty(s ctxbudget.Status) (mult float64, note string) {
	switch s {
	case ctxbudget.OverHard:
		return 0.3, "Always-loaded context is over the hard budget — too bloated for an agent to load; heavily penalized."
	case ctxbudget.OverSoft:
		return 0.6, "Always-loaded context is over the soft budget — trending bloated; penalized."
	default:
		return 1.0, "Always-loaded context is within budget."
	}
}

// --- setup reproducibility -------------------------------------------------

// taskRunner returns the path of a recognized task runner, or "". Beyond the
// dedicated runner files (Makefile/Taskfile/justfile) it recognizes
// language-native script runners — a non-empty "scripts" map in
// package.json/composer.json, or a Go cmd/<name>/main.go entrypoint — so a repo
// whose documented run path is `npm run dev` (not `make`) isn't a false negative.
func taskRunner(r *repo.Repo) string {
	if hit := r.FirstExisting("Makefile", "makefile", "Taskfile.yml", "Taskfile.yaml", "justfile", "Justfile"); hit != "" {
		return hit
	}
	for _, m := range []string{"package.json", "composer.json"} {
		if !r.Exists(m) {
			continue
		}
		if body, err := r.Read(m); err == nil && hasJSONScripts(body) {
			return m
		}
	}
	if mains := r.Glob("cmd/*/main.go"); len(mains) > 0 {
		return mains[0]
	}
	return ""
}

// hasJSONScripts reports whether a package.json/composer.json declares a
// non-empty top-level "scripts" object.
func hasJSONScripts(body string) bool {
	var m struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return false
	}
	return len(m.Scripts) > 0
}

// Setup reproducibility — clone -> running via one documented path?
func setupReproducibility(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatSetup)
	container := r.FirstExisting("Dockerfile", "docker-compose.yml", "compose.yml", ".devcontainer/devcontainer.json")
	runner := taskRunner(r)
	manifest := r.FirstExisting("go.mod", "package.json", "composer.json", "requirements.txt", "pyproject.toml", "Gemfile", "Cargo.toml")

	docSetup := false
	if readme := r.FirstExisting("README.md", "README.rst", "README.txt", "README"); readme != "" {
		body, _ := r.Read(readme)
		docSetup = hasSetupSection(body)
		if docSetup {
			c.Signals = append(c.Signals, evFound("documented setup section", scorecard.MethodContent, readme))
		}
	}
	for label, hit := range map[string]string{"container/devcontainer": container, "task runner": runner, "dependency manifest": manifest} {
		if hit != "" {
			c.Signals = append(c.Signals, evFound(label, scorecard.MethodFile, hit))
		} else {
			c.Signals = append(c.Signals, evAbsent(label, scorecard.MethodFile, ""))
		}
	}

	// Facet: an executable run command (issue #3). A documented setup *section* is
	// prose; this is a copy-pasteable command an agent can run without inferring.
	// It's the lever between "the repo has the pieces" and "it runs as written".
	runbookSrc, hasEndpoint, runbook := runnableRunbook(r)
	if runbook {
		note := "copy-pasteable run command in code"
		if hasEndpoint {
			note += " (with a reachable endpoint/port)"
		}
		c.Signals = append(c.Signals, evNote("runnable run command", scorecard.MethodContent, runbookSrc, note))
	} else {
		c.Signals = append(c.Signals, evAbsent("runnable run command", scorecard.MethodContent, ""))
	}

	complete := (container != "" && (runner != "" || manifest != "" || docSetup)) ||
		(runner != "" && manifest != "") ||
		(manifest != "" && docSetup)
	any := container != "" || runner != "" || manifest != "" || docSetup

	// Structural completeness sets the floor; the runnable runbook lifts it toward
	// full marks. A repo can't reach 1.0 on structure alone — without an executable
	// path an agent still has to infer how to start it.
	var base float64
	switch {
	case complete:
		base = setupStructuralComplete
	case any:
		base = setupStructuralPartial
	}
	if runbook {
		base += setupRunbookBonus
	}
	if base > 1 {
		base = 1
	}
	c.Normalized = base
	c.Rationale = "Structural signals (container / task-runner+manifest / manifest+docs) set the floor; a copy-pasteable run command lifts toward full marks. Judgment confirms a single documented path."
	return c
}

// setup-reproducibility scoring weights: structural completeness vs. the
// executable-runbook facet (issue #3). complete structure + a runnable command
// is the only path to full marks.
const (
	setupStructuralComplete = 0.75
	setupStructuralPartial  = 0.35
	setupRunbookBonus       = 0.25
)

// --- CI: test / build / deploy ---------------------------------------------

// CI pipeline — present and actually green.
func cicdPipeline(r *repo.Repo, gh githubclient.Client) scorecard.Category {
	c := newCategory(scorecard.CatCICD)
	workflows := r.Glob(".github/workflows/*.yml")
	workflows = append(workflows, r.Glob(".github/workflows/*.yaml")...)
	hasCI := len(workflows) > 0 || r.FirstExisting(".gitlab-ci.yml", "Jenkinsfile") != ""
	if !hasCI {
		c.Signals = append(c.Signals, evAbsent("CI workflow", scorecard.MethodFile, ""))
		c.Normalized, c.Rationale = 0, "No CI configuration found."
		return c
	}
	c.Signals = append(c.Signals, evNote("CI workflows", scorecard.MethodFile, ".github/workflows", fmt.Sprintf("%d workflow file(s)", len(workflows))))
	raw := 1 // present

	green := gh.LatestRunGreen(r.Slug)
	switch {
	case green.NoData:
		c.Signals = append(c.Signals, evNoData("latest CI run green", scorecard.MethodAPI, green.Reason))
		c.BlockedBy = append(c.BlockedBy, "latest-run status: "+green.Reason)
		c.Rationale = "CI present (partial credit); green-status unverified — judgment/API needed for full credit."
	case green.OK:
		c.Signals = append(c.Signals, evFoundDetail("latest CI run green", scorecard.MethodAPI, green.Detail))
		raw = 2
		c.Rationale = "CI present and latest run green."
	default:
		c.Signals = append(c.Signals, evNotFoundDetail("latest CI run green", scorecard.MethodAPI, green.Detail))
		c.Rationale = "CI present but latest run not green (capped at partial credit)."
	}
	c.Normalized = scaled(raw)
	return c
}

// --- config & secrets ------------------------------------------------------

// Config & secrets — including the secret floor that flags hardcoded credentials.
func configSecrets(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatConfigSecrets)
	envExample := r.FirstExisting(".env.example", ".env.sample", ".env.template")
	gi, _ := r.Read(".gitignore")
	ignoresEnv := containsAny(gi, ".env")

	if envExample != "" {
		c.Signals = append(c.Signals, evFound(".env.example present", scorecard.MethodFile, envExample))
	} else {
		c.Signals = append(c.Signals, evAbsent(".env.example present", scorecard.MethodFile, ""))
	}
	if ignoresEnv {
		c.Signals = append(c.Signals, evFound(".gitignore covers .env", scorecard.MethodContent, ".gitignore"))
	}

	hits := scanSecrets(r)
	if len(hits) > 0 {
		for _, h := range hits[:min(len(hits), 5)] {
			c.Signals = append(c.Signals, scorecard.Evidence{
				Signal: "hardcoded secret", Method: scorecard.MethodContent, Status: scorecard.StatusOK,
				Found: scorecard.Boolp(true), Path: h.Path, Ref: h.Ref, Source: "filesystem", Note: h.Rule,
			})
		}
		c.Normalized = scaled(1) // hard cap: secrets in source
		c.Rationale = fmt.Sprintf("Secret scan tripped on %d location(s); capped regardless of judgment.", len(hits))
		return c
	}
	c.Signals = append(c.Signals, evAbsent("secret scan hits", scorecard.MethodContent, ""))

	switch envVars := referencedEnvVars(r); {
	case envExample != "":
		c.Normalized = scaled(2)
		c.Rationale = "No secrets; configuration documented via example file."
	case len(envVars) >= 3:
		shown := envVars[:min(len(envVars), 8)]
		c.Signals = append(c.Signals, evNote("env vars referenced in code", scorecard.MethodContent, "", fmt.Sprintf("%d distinct: %v", len(envVars), shown)))
		c.Normalized = scaled(1)
		c.Rationale = fmt.Sprintf("No secrets, but %d config vars are consumed with no .env.example to document them.", len(envVars))
	default:
		c.Normalized = scaled(2)
		if len(envVars) > 0 {
			c.Rationale = fmt.Sprintf("No secrets; minimal config (%d env var(s)) — nothing requiring a documented sheet.", len(envVars))
		} else {
			c.Rationale = "No secrets; repo consumes no runtime configuration."
		}
	}
	return c
}

// --- purpose & orientation -------------------------------------------------

// Purpose & orientation — can a newcomer learn what/why/who fast?
func purposeOrientation(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatPurpose)
	readme := r.FirstExisting("README.md", "README.rst", "README.txt", "README")
	if readme == "" {
		c.Signals = append(c.Signals, evAbsent("README present", scorecard.MethodFile, ""))
		c.Normalized, c.Rationale = 0, "No README; nothing orients a newcomer."
		return c
	}
	body, _ := r.Read(readme)
	headings := countHeadings(body)
	c.Signals = append(c.Signals,
		evFound("README present", scorecard.MethodFile, readme),
		evNote("README size", scorecard.MethodContent, readme, fmt.Sprintf("%d bytes, %d headings", len(body), headings)),
	)
	raw := 1
	if len(body) >= 600 && headings >= 2 {
		raw = 2
	}
	c.Normalized = scaled(raw)
	c.Rationale = "Deterministic proxy on README presence/structure; judgment confirms it answers what/why/who."
	return c
}

// --- conventions & standards -----------------------------------------------

// Conventions & standards — written and enforced.
func conventionsStandards(r *repo.Repo, gh githubclient.Client) scorecard.Category {
	c := newCategory(scorecard.CatConventions)
	linters := []string{".editorconfig", ".eslintrc", ".eslintrc.json", ".eslintrc.js", ".prettierrc", "ruff.toml", ".golangci.yml", ".golangci.yaml", ".tflint.hcl", ".pre-commit-config.yaml"}
	var found []string
	for _, l := range linters {
		if r.Exists(l) {
			found = append(found, l)
		}
	}
	codeowners := r.FirstExisting("CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS")
	tags := r.GitTags()

	if len(found) > 0 {
		c.Signals = append(c.Signals, evNote("lint/format configs", scorecard.MethodFile, "", fmt.Sprintf("%v", found)))
	} else {
		c.Signals = append(c.Signals, evAbsent("lint/format configs", scorecard.MethodFile, ""))
	}
	if codeowners != "" {
		c.Signals = append(c.Signals, evFound("CODEOWNERS", scorecard.MethodFile, codeowners))
	}
	if len(tags) > 0 {
		c.Signals = append(c.Signals, evNote("git tags (semver signal)", scorecard.MethodGit, "", fmt.Sprintf("%d tag(s)", len(tags))))
	}

	prot := gh.BranchProtected(r.Slug)
	switch {
	case prot.NoData:
		c.Signals = append(c.Signals, evNoData("branch protection", scorecard.MethodAPI, prot.Reason))
		c.BlockedBy = append(c.BlockedBy, "branch protection — "+prot.Reason)
	case prot.OK:
		c.Signals = append(c.Signals, evFoundDetail("branch protection", scorecard.MethodAPI, prot.Detail))
	default:
		c.Signals = append(c.Signals, evNotFoundDetail("branch protection", scorecard.MethodAPI, prot.Detail))
	}

	raw := 0
	switch boolN(len(found) > 0, codeowners != "", len(tags) > 0, prot.OK) {
	case 0:
		raw = 0
	case 1:
		raw = 1
	default:
		raw = 2
	}
	c.Normalized = scaled(raw)
	c.Rationale = "Deterministic count of standards signals; judgment confirms they are enforced, not vestigial."
	return c
}

// --- source-material trail -------------------------------------------------

// Source-material trail — is the "why" recoverable? (links here; MCP resolves later)
func sourceMaterialTrail(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatSourceTrail)
	adr := r.FirstExisting("docs/adr", "docs/decisions", "adr", "doc/adr")
	readme := r.FirstExisting("README.md", "README")
	body := ""
	if readme != "" {
		body, _ = r.Read(readme)
	}
	links := countLinks(body)

	if adr != "" {
		c.Signals = append(c.Signals, evFound("ADR / decision records", scorecard.MethodFile, adr))
	} else {
		c.Signals = append(c.Signals, evAbsent("ADR / decision records", scorecard.MethodFile, ""))
	}
	c.Signals = append(c.Signals, evNote("Confluence/Jira links in README", scorecard.MethodContent, readme, fmt.Sprintf("%d link(s)", links)))
	// Actual link resolution (do the linked pages exist & explain why) is the
	// skill+MCP layer's job — not-yet-determined here.
	c.Signals = append(c.Signals, evNoData("linked source material resolved", scorecard.MethodSkill, "needs skill+atlassian MCP (out of binary scope)"))
	c.BlockedBy = append(c.BlockedBy, "link resolution: deferred to skill+MCP")

	raw := 0
	switch boolN(adr != "", links > 0) {
	case 0:
		raw = 0
	case 1:
		raw = 1
	default:
		raw = 2
	}
	c.Normalized = scaled(raw)
	c.Rationale = "Deterministic detection of links/ADRs; judgment+MCP confirms the why is actually recoverable."
	return c
}

// --- testing & coverage ----------------------------------------------------

// Testing & coverage — are there tests (clone -> verifiable) and is coverage
// reported? Generic-first file heuristics, with coverage artifacts per the
// detected stack's profile.
func testingAndCoverage(r *repo.Repo, stacks detect.Result) scorecard.Category {
	c := newCategory(scorecard.CatTesting)
	tests := hasTests(r)
	cov, covWhere := hasCoverage(r, stacks)

	if tests {
		c.Signals = append(c.Signals, evNote("test sources", scorecard.MethodFile, "", "test files/dirs detected"))
	} else {
		c.Signals = append(c.Signals, evAbsent("test sources", scorecard.MethodFile, ""))
	}
	if cov {
		c.Signals = append(c.Signals, evFound("coverage reporting", scorecard.MethodContent, covWhere))
	} else {
		c.Signals = append(c.Signals, evAbsent("coverage reporting", scorecard.MethodContent, ""))
	}

	switch {
	case tests && cov:
		c.Normalized = 1.0
		c.Rationale = "Tests present and coverage reported."
	case tests:
		c.Normalized = 0.5
		c.Rationale = "Tests present; no coverage reporting detected."
	default:
		c.Normalized = 0
		c.Rationale = "No tests detected."
	}
	return c
}

// --- in-repo tooling -------------------------------------------------------

// In-repo tooling — task runners, scripts, and agent skills that live in the
// repo so work is reproducible without tribal knowledge.
func inRepoTooling(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatInRepoTooling)
	runner := r.FirstExisting("Makefile", "Taskfile.yml", "Taskfile.yaml", "justfile", "Justfile")
	scriptsDir := r.FirstExisting("scripts", "tools", "hack")
	skills := r.FirstExisting(".claude/skills", ".claude/commands", ".cursor/commands")
	npmScripts := false
	if r.Exists("package.json") {
		if body, _ := r.Read("package.json"); strings.Contains(body, "\"scripts\"") {
			npmScripts = true
		}
	}

	report := func(label, hit string) {
		if hit != "" {
			c.Signals = append(c.Signals, evFound(label, scorecard.MethodFile, hit))
		} else {
			c.Signals = append(c.Signals, evAbsent(label, scorecard.MethodFile, ""))
		}
	}
	report("task runner", runner)
	report("scripts directory", scriptsDir)
	report("agent skills/commands", skills)
	if npmScripts {
		c.Signals = append(c.Signals, evFound("package.json scripts", scorecard.MethodContent, "package.json"))
	}

	switch boolN(runner != "", scriptsDir != "", skills != "", npmScripts) {
	case 0:
		c.Normalized, c.Rationale = 0, "No task runner, scripts, or agent skills in-repo."
	case 1:
		c.Normalized, c.Rationale = 0.5, "Some in-repo tooling; one kind present."
	default:
		c.Normalized, c.Rationale = 1.0, "Multiple kinds of in-repo tooling present."
	}
	return c
}

// --- dependency patching ---------------------------------------------------

// Dependency patching — automated updates (Dependabot/Renovate) over pinned
// dependencies. Not-applicable when the repo has no dependency ecosystem at all.
func dependencyPatching(r *repo.Repo, stacks detect.Result) scorecard.Category {
	c := newCategory(scorecard.CatDependencyPatching)
	updater := r.FirstExisting(".github/dependabot.yml", ".github/dependabot.yaml",
		"renovate.json", ".renovaterc", ".renovaterc.json", ".github/renovate.json")

	var lockfile string
	for _, id := range stacks.IDs() {
		if lockfile = r.FirstExisting(detect.ProfileFor(id).LockFiles...); lockfile != "" {
			break
		}
	}
	if lockfile == "" {
		lockfile = r.FirstExisting("go.sum", "package-lock.json", "yarn.lock", "composer.lock", "Gemfile.lock", "Cargo.lock", "poetry.lock", "Pipfile.lock", ".terraform.lock.hcl")
	}
	manifest := r.FirstExisting("go.mod", "package.json", "composer.json", "requirements.txt", "pyproject.toml", "Gemfile", "Cargo.toml")

	// No dependency ecosystem => not-applicable (weight redistributes).
	if stacks.Undetermined() && manifest == "" && lockfile == "" {
		c.Applicable = false
		c.Signals = append(c.Signals, scorecard.Evidence{
			Signal: "dependency ecosystem", Method: scorecard.MethodFile,
			Status: scorecard.StatusNotApplicable, Source: "filesystem",
			Note: "no manifest, lockfile, or detected stack — nothing to patch",
		})
		c.Rationale = "Not applicable: no dependency ecosystem detected."
		return c
	}

	if updater != "" {
		c.Signals = append(c.Signals, evFound("automated dependency updates", scorecard.MethodFile, updater))
	} else {
		c.Signals = append(c.Signals, evAbsent("automated dependency updates", scorecard.MethodFile, ""))
	}
	if lockfile != "" {
		c.Signals = append(c.Signals, evFound("dependency lockfile", scorecard.MethodFile, lockfile))
	} else {
		c.Signals = append(c.Signals, evAbsent("dependency lockfile", scorecard.MethodFile, ""))
	}

	switch {
	case updater != "":
		c.Normalized, c.Rationale = 1.0, "Automated dependency updates configured."
	case lockfile != "":
		c.Normalized, c.Rationale = 0.5, "Dependencies pinned via lockfile, but no automated patching."
	default:
		c.Normalized, c.Rationale = 0, "Dependencies present but neither pinned nor auto-patched."
	}
	return c
}

// --- DB migrations ---------------------------------------------------------

// DB migrations — when the repo uses a database, are schema changes managed via
// migrations? Not-applicable when no database is detected (the showcase for the
// not-applicable state: a DB-less repo isn't punished for having no migrations).
func dbMigrations(r *repo.Repo) scorecard.Category {
	c := newCategory(scorecard.CatDBMigrations)
	migrations := hasMigrations(r)
	driver, driverWhere := hasDBDriver(r)

	if !migrations && driver == "" {
		c.Applicable = false
		c.Signals = append(c.Signals, scorecard.Evidence{
			Signal: "database usage", Method: scorecard.MethodFile,
			Status: scorecard.StatusNotApplicable, Source: "filesystem",
			Note: "no DB driver or migrations detected — category does not apply",
		})
		c.Rationale = "Not applicable: no database detected."
		return c
	}

	if driver != "" {
		c.Signals = append(c.Signals, evNote("database driver/ORM", scorecard.MethodContent, driverWhere, driver))
	}
	if migrations {
		c.Signals = append(c.Signals, evFound("migrations", scorecard.MethodFile, "migrations"))
		c.Normalized, c.Rationale = 1.0, "Database in use with managed migrations."
	} else {
		c.Signals = append(c.Signals, evAbsent("migrations", scorecard.MethodFile, ""))
		c.Normalized, c.Rationale = 0, "Database in use but no migrations found."
	}
	return c
}

// --- detection helpers for the categories above ----------------------------

var testDirSegments = map[string]bool{"test": true, "tests": true, "spec": true, "__tests__": true}

func hasTests(r *repo.Repo) bool {
	for _, rel := range r.Files() {
		low := strings.ToLower(rel)
		base := strings.ToLower(filepath.Base(rel))
		switch {
		case strings.HasSuffix(low, "_test.go"),
			strings.HasSuffix(low, ".test.js"), strings.HasSuffix(low, ".test.jsx"),
			strings.HasSuffix(low, ".test.ts"), strings.HasSuffix(low, ".test.tsx"),
			strings.HasSuffix(low, ".spec.js"), strings.HasSuffix(low, ".spec.ts"),
			strings.HasSuffix(low, "_test.py"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
			strings.HasSuffix(base, "test.php"),
			strings.HasSuffix(low, "_spec.rb"), strings.HasSuffix(low, "_test.rb"),
			strings.HasSuffix(low, "_test.exs"), strings.HasSuffix(low, "_test.rs"):
			return true
		}
		for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if testDirSegments[strings.ToLower(seg)] {
				return true
			}
		}
	}
	return false
}

var coverageConfigs = []string{"codecov.yml", ".codecov.yml", "codecov.yaml", ".github/codecov.yml", ".coveralls.yml", ".coveragerc"}
var coverageKeywords = []string{"codecov", "coveralls", "coverage", "-cover", "--cov", "go test -cover"}

func hasCoverage(r *repo.Repo, stacks detect.Result) (bool, string) {
	if f := r.FirstExisting(coverageConfigs...); f != "" {
		return true, f
	}
	for _, id := range stacks.IDs() {
		for _, g := range detect.ProfileFor(id).CoverageGlobs {
			if r.Exists(g) || len(r.Glob(g)) > 0 {
				return true, g
			}
		}
	}
	workflows := append(r.Glob(".github/workflows/*.yml"), r.Glob(".github/workflows/*.yaml")...)
	for _, p := range append(workflows, "Makefile") {
		body, err := r.Read(p)
		if err != nil {
			continue
		}
		if containsAny(strings.ToLower(body), coverageKeywords...) {
			return true, p
		}
	}
	return false, ""
}

func hasMigrations(r *repo.Repo) bool {
	if r.FirstExisting("alembic.ini", "knexfile.js", "knexfile.ts") != "" {
		return true
	}
	for _, rel := range r.Files() {
		// Require a "migrations" directory (or Rails' db/migrate) — not any path
		// segment containing "migrate", which matches unrelated code packages.
		low := filepath.ToSlash(strings.ToLower(rel))
		if strings.HasPrefix(low, "migrations/") || strings.Contains(low, "/migrations/") || strings.Contains(low, "db/migrate/") {
			return true
		}
	}
	return false
}

// containsToken reports whether token appears in haystack at word boundaries,
// so e.g. "sequel" doesn't match inside "sequelize". Word chars are [a-z0-9_];
// path separators, quotes, and punctuation are boundaries.
func containsToken(haystack, token string) bool {
	low, t := strings.ToLower(haystack), strings.ToLower(token)
	for from := 0; ; {
		i := strings.Index(low[from:], t)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isWordByte(low[i-1])
		end := i + len(t)
		afterOK := end >= len(low) || !isWordByte(low[end])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

var dbDriverTokens = []string{
	"lib/pq", "jackc/pgx", "go-sql-driver/mysql", "gorm.io", "mattn/go-sqlite3", // go
	"pg", "mysql2", "sqlite3", "sequelize", "prisma", "typeorm", "knex", "mongoose", // node
	"doctrine", "illuminate/database", "ext-pdo", "ext-pdo_mysql", "ext-pdo_pgsql", // php
	"psycopg2", "sqlalchemy", "asyncpg", "pymysql", "alembic", "django", // python
	"activerecord", "sequel", // ruby
}

func hasDBDriver(r *repo.Repo) (string, string) {
	manifests := []string{"go.mod", "package.json", "composer.json", "requirements.txt", "pyproject.toml", "Pipfile", "Gemfile"}
	for _, m := range manifests {
		body, err := r.Read(m)
		if err != nil {
			continue
		}
		for _, tok := range dbDriverTokens {
			if containsToken(body, tok) {
				return tok, m
			}
		}
	}
	return "", ""
}
