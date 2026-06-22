# ADR 0002 — Rubric v2: weighted categories, /100, two-layer scoring

Status: accepted · Date: 2026-06-22 · Supersedes the v0.1 rubric in ADR-0001

## Context

The v0.1 rubric (ADR-0001) is 7 flat dimensions scored 0–2, max 14. Calibration runs
and adoption planning surfaced four limits:

1. **Flat scoring hides what matters.** Every dimension counts equally. An adopter
   can't say "testing matters more to us than the source-material trail."
2. **The math is awkward.** A 14-point scale makes thresholds and weighting
   unintuitive to reason about and communicate.
3. **The category set is too coarse.** "CI/CD legibility" conflates testing,
   coverage, build, deploy, dependency patching, and migrations — each a distinct,
   separately-fixable mechanic. The rubric should reward the *mechanics of a
   well-run CI/CD platform around the code* AND the *agent/human instructions that
   explain how to use them*.
4. **No notion of applicability.** A repo with no database scoring 0 on "DB
   migrations" is wrong — it's not a gap, it's not applicable. Agnosticism demands
   the rubric distinguish "missing" from "doesn't apply here."

`toaster-ready` is also positioned as an agnostic, opinionated, open-source tool: zero
config yields the maintainer's defaults; everything is overridable. And the CI path
must stay **CLI-only and deterministic** — no agent required to score.

## Decisions

1. **Score out of 100, weighted categories.** Each category carries a weight; the
   default weights sum to 100. A category produces a normalized subscore in `[0,1]`
   from its signals; its **contribution = weight × normalized**. The repo score is
   the sum of contributions (see §Scoring math for redistribution). `/100` makes
   weighting and thresholds intuitive.

2. **The category set (v2).** Eleven categories, replacing the seven flat
   dimensions. Default weights (opinionated; fully overridable via config):

   | Category | ID | Weight | Notes |
   |----------|----|-------:|-------|
   | Agent/human instructions | `agent-instructions` | 15 | three facets (decision 3) |
   | Setup reproducibility | `setup-reproducibility` | 12 | clone → running, one path |
   | Testing & coverage | `testing-and-coverage` | 12 | tests exist + coverage reported |
   | CI: test / build / deploy | `cicd-pipeline` | 12 | jobs present and green |
   | Config & secrets | `config-and-secrets` | 10 | `.env.example`; no secrets in source |
   | Purpose & orientation | `purpose-and-orientation` | 10 | README answers what/why/who |
   | Conventions & standards | `conventions-and-standards` | 8 | linters, CODEOWNERS, semver, protection |
   | Source-material trail | `source-material-trail` | 7 | the *why* is recoverable (ADRs, links) |
   | In-repo tooling | `in-repo-tooling` | 6 | Makefile / scripts / skills |
   | Dependency patching | `dependency-patching` | 5 | Dependabot/Renovate or equivalent |
   | DB migrations | `db-migrations` | 3 | **conditional** — N/A when no DB detected |
   | **Total** | | **100** | |

   The category set is **fixed** in v2 (opinionated standard, comparable across
   repos). Config tunes weights/thresholds/signals — not the category list.
   Custom/pluggable categories are explicitly post-MVP.

3. **Two-layer scoring; agent-instructions is its own category with three facets.**
   For each mechanic the rubric asks (1) *does it exist?* and (2) *is it documented
   so an agent/human can use it without asking?* Layer (2) is concentrated in the
   `agent-instructions` category, scored across three facets:
   - **exists** — `CLAUDE.md`/`AGENTS.md` (etc.) present;
   - **explains the mechanics** — points at the real test/build/deploy/migration/
     tooling commands the other categories detect;
   - **fits a context budget** — the always-loaded footprint is within budget; bloat
     is penalized.

4. **Applicability is a first-class state.** Extending the no-data discipline, a
   category (or signal) may be **`not-applicable`** — a *determination* that the
   mechanic doesn't apply to this repo (no DB → migrations N/A; no deployable
   artifact → deploy sub-signal N/A). `not-applicable` is distinct from `no-data`
   ("we couldn't check") and from a real miss ("absent"). N/A categories are dropped
   from scoring and their weight redistributed (§Scoring math). The three signal
   states are now: `ok` · `no-data` · `not-applicable`.

5. **Language-aware via a detection seam (hybrid, generic-first).** Detect the stack,
   run generic heuristics by default (any CI job named test/build/deploy, any coverage
   artifact, any migrations directory), and layer language-specific detail in where
   available. Detection drives both applicability (decision 4) and which signals fire.
   The detected stack is reported in the scorecard.

6. **Bands re-expressed on /100** (proportional to the v0.1 bands):
   - **needs-work** 0–49 · **functional** 50–84 · **exemplary** 85–100.

   This preserves the calibration: a 7/14 (≥50%) stays in the functional band, and the
   adoption baseline "≥7/14" re-expresses cleanly as **≥50/100**.

7. **CLI-only / deterministic; judgment layer stays post-MVP.** No category requires
   an agent or LLM to score. Judgment-heavy checks (e.g. "does the README *read*
   well") use deterministic heuristic proxies (section presence, headings, length);
   true prose-quality judgment remains the optional skill layer's job and is never on
   the CI path. The `Judgment`/`Ceiling` mechanism from v0.1 is retained per category:
   the deterministic pass sets a conservative subscore and a ceiling, and a later
   judgment pass may raise the subscore toward the ceiling.

8. **Recommendations are emitted for low-scoring categories.** Each category scoring
   below a configurable level carries a structured recommendation — a *cause*
   (`miss` / `no-data` / `improve`) and a *suggested action* — keyed off the failing
   signals, templated and deterministic. Carried in the JSON and rendered by the
   Markdown/HTML output.

9. **`rubricVersion` → `2.0`; schema is a breaking change.** Pre-1.0 with no external
   consumers, so we bump and move on. The schema evolves from flat `Dimension` to
   weighted `Category`, anticipating context-budget metrics and recommendations:

   ```
   Scorecard {
     repo, ref, scoredAt, rubricVersion, scorer
     score        float   // 0–100, after redistribution
     band         string
     dataComplete bool
     detectedStack []string
     categories   []Category
   }
   Category {
     id, title
     weight       float   // default per decision 2; from config otherwise
     applicable   bool    // false => not-applicable, dropped + redistributed
     normalized   float   // [0,1] over determinable, applicable signals
     contribution float   // weight × normalized (0 when dropped)
     dataComplete bool
     blockedBy    []string
     signals      []Evidence        // optional Metrics carry e.g. context-budget tokens
     recommendations []Recommendation  // populated when below threshold
     rationale    string
   }
   Evidence { ...v1 fields..., status: ok|no-data|not-applicable, metrics?: map }
   Recommendation { category, cause: miss|no-data|improve, action, evidenceRef }
   ```

## Scoring math

Let `S` = the set of categories that are **applicable and not fully no-data**.

```
score = 100 × Σ_{c∈S} (weight_c × normalized_c) / Σ_{c∈S} weight_c
```

- A **not-applicable** category (decision 4) is excluded from `S`; its weight leaves
  the denominator, redistributing proportionally across scored categories. The repo
  is neither rewarded nor punished for a mechanic it doesn't need.
- A category that is **fully no-data** is likewise excluded, but flagged distinctly
  (`dataComplete:false`, `blockedBy`) so a reader sees the score is on partial
  information — not the same as N/A.
- **Within** a category, `normalized` is computed only over its determinable,
  applicable signals; no-data signals never count as 0.
- The scorecard reports, for auditability: which categories were dropped, why (N/A
  vs no-data), and the effective weight denominator used.

## Consequences

- **`internal/scorecard`** moves from `Dimension` (Score/Ceiling 0–2) to weighted
  `Category`; `Band()` switches to the /100 thresholds; `Max` becomes 100. **`gate`**
  gains a configurable score threshold over the existing essentials floor.
- **`internal/check`** restructures: `cicd-legibility` splits into
  `testing-and-coverage`, `cicd-pipeline`, `dependency-patching`, and `db-migrations`;
  `in-repo-tooling` is new; `agent-instructions` gains the context-budget facet.
  Detection is consulted before per-category checks run.
- **Default weights are a calibration target, not a fact** — they ship as the
  maintainer's opinion and get tuned against real scores before being treated as settled.
- `toaster-ready` must re-pass its own `gate` and score well on the v2 rubric (ADR-0001
  decision 2: born exemplary).

## Status note

Accepted: the category set, /100 weighting, and schema have shipped and survived a
calibration pass across real repos. The default weights remain open to tuning.
