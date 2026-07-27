# ADR 0003 — The secret floor stays basic; gitleaks runs in the pipeline

Status: accepted · Date: 2026-07-26 · Supersedes decision 7 of ADR-0001

## Context

ADR-0001 decision 7 committed to a "regex secret floor for v0.1, swappable for
gitleaks when wiring the real CI gate". The README carried the same promise. This
records why that swap is not happening, and what replaced it.

Two things changed since v0.1.

**The blast radius grew.** `go-shared-build`'s reusable `go-ci.yml` now runs the
toaster gate on every Go repo in the org. A secret hit is a *hard* gate failure,
independent of the score threshold — so this scanner became the fleet's secret
detection by default rather than one signal among several.

**Parity was measured, not estimated.** Reimplementing gitleaks in this binary means
its rule model (regex, `secretGroup`, `path` filters, `keywords` prefilters,
composite `RequiredRules`, and per-rule allowlists with match conditions and regex
targets) plus the engine around it:

| Package | Non-test LOC |
| --- | --- |
| `detect` | 1,559 |
| `config` | 818 |
| `detect/codec` — decodes base64/hex/percent-encoded content and rescans it | 924 |

~3,300 lines of security-critical matching, then tracked against upstream forever.

Importing gitleaks as a library instead was also measured: it takes this module
graph from **13 to 202 modules**. Binary size barely moves (9.8M → ~10M), so the
real cost is dependency surface — on code that executes in every project's CI,
which is the surface the SHA-pinning work exists to shrink.

## Decisions

1. **The built-in floor stays deliberately basic.** Its job is a *scoring* signal —
   "does this repo obviously have credentials committed?" — not to be a
   secret-scanning product. Few rules, high signal.

2. **gitleaks runs in the pipeline, pinned as a tool.** `go-shared-build` v0.8.0
   pins it in `tools.mk` and exposes `make secrets` (working tree, part of `check`)
   and `make secrets-history` (full history, its own CI job). As a *tool* it costs a
   consumer's `go.mod` nothing — the 202-module figure only applied to importing it.

3. **Do not import gitleaks as a library, and do not vendor its rules.** Vendoring
   the 222 default rules would still mean owning the matching semantics — 221 of
   them use `keywords`, 130 use `entropy`, 16 carry allowlists — and a subtle bug
   there is a *false negative in security tooling*, which is worse than the
   dependency tree it avoids. The encoded-content decoders would be missed entirely.

4. **Entropy gates the broad rules.** Name-based rules
   (`assigned-credential`, `credential-ish-name`) exist only because a Shannon
   entropy check on the value keeps them from flooding the score.

5. **Entropy is judged on the value the rule captured**, never guessed from the
   line. `map[string]string{"password": "…"}` would otherwise be scored on
   `"string"` and wrongly pass.

6. **Shape guards where entropy cannot help.** Kebab/snake identifiers of dictionary
   words are ignored: `config-and-secrets` scores **3.61**, *higher* than the real
   credential `hX7qP2mZ9vLk` at **3.58**. No threshold separates them, so the guard
   is on shape. Segments must be purely alphabetic — allowing digits made
   `sk_live_<random>` look like an identifier and silently stopped detecting
   it.

7. **The working tree only.** Git history is gitleaks' territory; a secret committed
   and later deleted is not something this floor tries to find.

## Consequences

- The roadmap item "swap the regex secret floor for gitleaks" is withdrawn.
  `.gitleaks.toml` stays — gitleaks now runs against this repo through
  `make secrets`, and that file allowlists the fake credentials in
  `internal/check/testdata/`.
- Two categories of miss are accepted by design: encoded secrets (no decoders), and
  anything needing history. Both are covered in the pipeline.
- Detection quality is now two independent layers rather than one. The floor
  answering "obviously committed credentials?" and gitleaks answering "any known
  secret pattern, anywhere in history?" fail differently, which is the point.
- One limitation worth stating plainly: entropy-based detection is blind to weak
  passwords. A short, wordlike credential clears no entropy bar and is invisible to
  both this floor and gitleaks. Periodically grepping for lines shaped like
  `password:` remains worthwhile.
