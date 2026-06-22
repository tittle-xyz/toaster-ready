# ADR 0001 — toaster-ready v0.1 design

Status: accepted · Date: 2026-06-17

## Context

AI-assisted development accelerates how fast code gets written, but it sharpens
a durable risk: knowledge concentration, bus factor, and over-reliance. The
concrete mitigation is making every repository fast to ramp up on — for a new
teammate *or* an agent. `toaster-ready` is the instrument that measures that, so "easy to
onboard onto" becomes an auditable standard rather than a vibe.

## Decisions

1. **Language: Go + Cobra.** Single static binary, no runtime to install on
   whatever scores repos in CI. The deterministic layer only reads files, runs
   regex, shells to `git`, and calls the GitHub API — all clean in Go.

2. **Own repo, born exemplary.** `toaster-ready` lives on its own so it can dogfood the
   rubric — it must pass its own `gate` and score well on its own scorecard.

3. **The binary is pure.** It reads a repo and prints a scorecard to stdout. No
   judgment of prose quality, no Confluence/Jira link resolution, no
   persistence. Those belong to a later **skill layer** (which adds judgment
   scoring, resolves linked source material via an MCP integration, and writes
   `scorecards/<slug>.json`). Keeping the binary pure makes it testable and
   CI-safe.

4. **No-data is a first-class third state, never zero.** Signals are `ok`
   (found/absent) or `no-data` (+reason). A dimension blocked by no-data reports
   `dataComplete:false` and `blockedBy`. A non-admin 403 on branch protection is
   no-data, not a 0. This is the auditability principle applied to the tool.

5. **GitHub access behind a narrow `Client` interface.** Two methods
   (`LatestRunGreen`, `BranchProtected`). Backends: a no-data `Stub` (offline +
   tests) and `GoGitHub` (go-github). Token from `GITHUB_TOKEN` with a
   `gh auth token` bootstrap. The interface keeps dimensions 4–5 testable
   without a network and the backend swappable.

6. **Deterministic ceiling, judgment within it.** Hard facts cap a dimension's
   achievable score; the deterministic layer sets `Score` conservatively and a
   later judgment pass may raise it toward `Ceiling`.

7. **Regex secret floor for v0.1**, swappable for gitleaks when wiring the real
   CI gate. It scans the working tree only — not git history.

8. **`git clone` via exec** for remote slugs (go-git deferred).

## Consequences

- v0.1 is deterministic-only; the judgment/skill layer and linked-source
  resolution are explicitly out of scope and surface as no-data.
- The rubric is defined in a Go struct and will be ratified after calibration
  runs, not before — building the tool sharpens the rubric.
