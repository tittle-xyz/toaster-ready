# toaster-ready — agent instructions

`toaster-ready` is a Go + Cobra CLI that scores how easy a repo is to ramp up on and emits a cited JSON scorecard. Read `README.md` for the product framing; this file is how to work in the code.

## The one rule that governs everything: no-data is not zero

A signal is three-state — `ok` (a real determination: found or absent) or `no-data` (could not be determined, with a `reason`). **Never collapse no-data into a score of 0.** When a check can't be made (permission 403, API error, deferred to a later layer), the dimension sets `dataComplete: false` and appends to `blockedBy`. This is the auditability principle turned on the tool itself: the scorecard tells the truth about what it couldn't measure. If you add a check that can fail to determine its answer, it MUST surface no-data, not guess.

## Layout

```
main.go                       entrypoint -> cmd.Execute()
cmd/                          Cobra commands: root, check, gate
internal/scorecard/           output schema + Band(); the no-data types live here
internal/repo/                repo acquisition (local path or shallow git clone) + fs/git facts
internal/check/               the 7 dimension checkers + helpers (secret floor, evidence ctors)
internal/githubclient/        narrow Client interface; Stub (no-data) + GoGitHub (go-github) backends
docs/adr/                     design decisions
```

## Architecture invariants — keep these true

- **The binary is pure.** `toaster-ready` reads a repo and prints a scorecard. It does NOT judge prose quality, resolve Confluence/Jira links, or persist results. Those belong to a future skill layer. Don't add persistence or LLM calls here.
- **GitHub access stays behind `githubclient.Client`.** Two methods today (`LatestRunGreen`, `BranchProtected`). This keeps dimensions 4–5 testable without a network and the backend swappable. Don't call go-github directly from a checker.
- **Deterministic checks set a ceiling; judgment fills within it.** A checker's `Ceiling` is the max still achievable from hard facts (no `CLAUDE.md` → ceiling 0). `Score` is the deterministic assessment. Dimensions marked `Judgment` leave room for the skill layer to raise `Score` toward `Ceiling` later.

## Adding a dimension or signal

1. Add a checker func in `internal/check/check.go` returning a `scorecard.Dimension`; wire it into `Run`'s slice.
2. Use the `ev*` evidence constructors in `helpers.go` so provenance (path, ref, method, source) is consistent.
3. If a signal needs GitHub, add a method to `githubclient.Client`, implement it in BOTH `Stub` (return `NoData`) and `GoGitHub`.
4. Update the rubric table in `README.md` and bump `RubricVersion` if scoring changed.

## Build / test / lint

```sh
make build      # go build -o ./bin/toaster .
make test       # go test ./...
make check      # vet + gofmt-check + test  (run before committing)
make gate       # toaster-ready scores ITSELF and must pass its own gate
```

## Conventions

- Names: lowercase + separators; never camelCase in user-facing identifiers (dimension IDs are kebab-case).
- Keep it gofmt-clean and `go vet`-clean; CI enforces both.
- No new dependencies without reason — stdlib + Cobra + go-github is the whole tree.
- Errors from the GitHub layer never fail a scoring run — they become no-data.

## Gotchas

- The secret scan reads the **working tree only**, not git history. A clean tree can still have secrets in history.
- The file walk (`repo.Files()`) skips what **git ignores** — a gitignored `.env` is not part of the repo, and scanning it made the same commit score differently locally than in CI. Ignore data comes from `git status --ignored`, which is index-aware, so a file that matches an ignore pattern but was *committed anyway* is still walked and still flagged. A non-git tree has no ignore rules to consult and is walked whole.
- `toaster gate` uses the offline stub by design — it's a pure local floor, so its printed `total` excludes live CI; the pass/fail decision is based on critical-dimension presence + the secret cap, not the total.
