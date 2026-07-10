# toaster-ready

[![ci](https://github.com/tittle-xyz/toaster-ready/actions/workflows/ci.yml/badge.svg)](https://github.com/tittle-xyz/toaster-ready/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/tittle-xyz/toaster-ready)](https://github.com/tittle-xyz/toaster-ready/releases)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Score how ready a repository is to ramp up on — for a new hire *or* an AI agent — and get a cited, provenance-bearing scorecard out of 100.**

> *Is your repo toaster-ready?* — yes, the "toasters" are the agents.

`toaster-ready` makes "easy to ramp up on" a **measurable, auditable property** of a repository rather than a vibe. Fast onboarding — for a person or an agent — is the concrete mitigation for the knowledge-concentration, bus-factor, and over-reliance risks that come with AI-assisted development. A repo that's hard to get productive in is a liability no matter how well it runs.

The CLI is `toaster`.

## What it does

`toaster` reads a repository and scores it against a weighted rubric, out of 100. It is **deterministic and pure** — it reads files, runs `git`, and (optionally) calls the GitHub API, then prints a cited scorecard. No LLM, no agent in the scoring path: it runs anywhere, including untrusted CI. Judgment scoring, link resolution, and persistence belong to an optional skill layer that wraps it.

```sh
toaster check .             # cited scorecard (JSON) for the current repo
toaster check owner/repo    # clone + score a remote repo
toaster gate . --min 50     # CI gate: non-zero exit below the bar
```

## The rubric

Eleven weighted categories, scored 0–100. Each category yields a normalized subscore; its contribution is `weight × subscore`; the total is the sum.

| Category | Weight | Looks for |
|---|--:|---|
| Agent/human instructions | 15 | `CLAUDE.md`/`AGENTS.md` that explain the mechanics, **fit a context budget** (bloat is penalized), and **stay true to the code** — stale-vs-churn and broken `make`/`npm`/`just` command references are penalized (presence ≠ accuracy) |
| Setup reproducibility | 12 | clone → running via one documented path — full marks need a **copy-pasteable run command** (e.g. `docker compose up`, `npm run dev`, `make run`), not just a prose setup section |
| Testing & coverage | 12 | tests exist and coverage is reported |
| CI: test / build / deploy | 12 | pipeline present and actually green |
| Config & secrets | 10 | `.env.example` present; no secrets in source |
| Purpose & orientation | 10 | README answers what / why / who |
| Conventions & standards | 8 | linters, CODEOWNERS, semver, branch protection |
| Source-material trail | 7 | the *why* is recoverable (ADRs, linked decisions) |
| In-repo tooling | 6 | task runner / scripts / agent skills |
| Dependency patching | 5 | Dependabot/Renovate over a lockfile |
| DB migrations | 3 | local datastore provisioning — managed migrations (core) plus a compose **DB service** to bring it up and a **seed** script to populate it (N/A when there's no DB) |

**Bands:** `0–49` needs-work · `50–84` functional · `85–100` exemplary.

Weights, thresholds, and signals are **configurable** (see [Configuration](#configuration)); the category set is fixed so scores stay comparable across repos.

## Three-state signals: never guess

Every signal is `ok` (a real determination — found or absent), `no-data` (couldn't be checked, with a reason), or `not-applicable` (doesn't apply to this repo).

- **`no-data` is never scored 0.** A category blocked by no-data reports `dataComplete: false` and what blocked it — so you can always tell *"we checked and it's missing"* from *"we couldn't check."* (A 403 reading branch protection without an admin token is no-data, not a zero.)
- **`not-applicable` is dropped, not penalized.** A repo with no database isn't docked for "no migrations" — the category is excluded and its weight redistributes across the rest.
- **Every score cites evidence** (path, locator, method). Provenance is the point.

## Install

```sh
go install github.com/tittle-xyz/toaster-ready/cmd/toaster@latest
```

Or build from source:

```sh
git clone https://github.com/tittle-xyz/toaster-ready
cd toaster-ready && make build   # -> ./bin/toaster
```

## Usage

```sh
toaster check <path|owner/repo>      # cited scorecard to stdout
  --offline                          # skip the GitHub API (API signals -> no-data)
  --format json|markdown|html        # output format (default: json)
  --config <path>                    # config file (default: .toaster-ready.yml at the root)

toaster gate <path|owner/repo>       # CI gate: non-zero exit on failure
  --min <0-100>                      # minimum score to pass (overrides config; default 50)
  --config <path>

toaster config <path|owner/repo>     # print the resolved config (defaults + overrides)
toaster detect <path|owner/repo>     # print the detected language/stack
```

A `owner/repo` slug is shallow-cloned via `git`. Live signals (CI status, branch protection) use the GitHub API; the client resolves a token from `GITHUB_TOKEN`, falling back to `gh auth token`. With no token it still works on public repos; auth-only facts surface as no-data. **`gate` runs offline-only by design**, so it needs no secrets in CI.

## GitHub Action

Gate any repo's CI on ramp-up readiness — the scorecard is written to the job summary, and the step fails if the repo is below the threshold or misses an essential (README / agent instructions / CI, or a hardcoded secret):

```yaml
- uses: actions/checkout@v4
- uses: tittle-xyz/toaster-ready@v0   # or pin a full version, e.g. @v0.2.0
  with:
    min: 50          # fail below this score (optional; default uses config, else 50)
    # target: .      # path or owner/repo (default: the checked-out repo)
    # config: .toaster-ready.yml
```

## Configuration

Drop a `.toaster-ready.yml` at the repo root to override the defaults. With no config, the built-in (opinionated) defaults apply. Everything is optional:

```yaml
weights:                    # override any category weight (relative)
  testing-and-coverage: 20
disabled:                   # skip categories entirely
  - db-migrations
languages:                  # hint the stack if detection misses it
  - php
contextBudget:              # always-loaded agent-context token budget
  soft: 6000
  hard: 16000
gate:
  threshold: 50             # toaster gate pass bar
recommend:
  below: 0.75               # emit recommendations for categories below this
```

Unknown category ids are rejected — the category set is fixed.

## Output

A single JSON document per repo, pinned to the scored git SHA, with a timestamp and rubric version. Categories carry their weight, normalized subscore, contribution, cited evidence, and — for anything below the bar — **recommendations** (cause + what to do). `--format markdown|html` renders the same data for PR comments or job summaries.
`--format shields` emits a [shields.io endpoint](https://shields.io/badges/endpoint-badge) JSON so a repo can show a live readiness badge — write it somewhere with a stable raw URL (a committed file, a gist, or gh-pages) and reference it:

```sh
toaster check . --offline --format shields > badge.json
# ![toaster-ready](https://img.shields.io/endpoint?url=<raw-url-of-badge.json>)
```

## How it's built

`toaster-ready` is built **AI-assisted and human-reviewed**, and it's transparent about that — it's a tool *about* agent-readiness, so it practices what it measures. The rigor behind it: decisions recorded as [ADRs](docs/adr/), a deterministic-by-design core (no LLM in the scoring path), real test coverage, an adversarial security/correctness/quality review before release, and — the proof — **`toaster-ready` scores itself**, and you can read the cited result. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Roadmap

- **Skill layer** — an optional agent-driven wrapper that adds judgment scoring, resolves linked source material (e.g. Confluence/Jira) via an MCP integration, and persists `scorecards/<slug>.json`. The binary stays pure.
- **GitHub Action** — drop-in CI adoption.
- **gitleaks** — swap the regex secret floor for a full scanner.

## License

[Apache-2.0](LICENSE) © Drew Tittle.
