APP  := toaster
MAIN := ./cmd/toaster

# `run` predates the shared build and is documented as the one-command demo, so
# keep this repo's version rather than the generic one.
SHARED_SKIP_TARGETS := run

SHARED_BUILD_REPO ?= https://github.com/tittle-xyz/go-shared-build.git
SHARED_BUILD_REF  ?= v0.9.1
SHARED_BUILD_DIR  ?= .build

# Fetch the pinned shared build before the include below is evaluated, so a fresh
# clone needs nothing but `make`. Done at parse time rather than as a rule for the
# include: GNU Make 3.81 (what macOS ships) warns about the missing file before it
# would remake it, and the warning reads like an error.
_ := $(shell test -f $(SHARED_BUILD_DIR)/go.mk || { \
       git -c advice.detachedHead=false clone --quiet --depth 1 \
         --branch $(SHARED_BUILD_REF) \
         $(SHARED_BUILD_REPO) $(SHARED_BUILD_DIR) >&2 && \
       rm -rf $(SHARED_BUILD_DIR)/.git; })

# Plain `include`, not `-include`: if the fetch above failed, fail here with a real
# error rather than silently continuing with no targets defined.
include $(SHARED_BUILD_DIR)/go.mk

# ---- repo-specific ----------------------------------------------------------

# Clone -> see it work, in one command: score this repo with this repo.
.PHONY: run
run: build ## Score this repo with this repo
	$(BINARY) check .

# Dogfood: toaster-ready must clear its own ramp-up floor. Uses the binary built
# from THIS tree, not a published release — which is why ci.yml sets
# `readiness: false` and runs this instead of the shared readiness job.
.PHONY: gate
gate: build ## Score this repo and fail below the ramp-up floor
	$(BINARY) gate .

# Dogfood the badge. Offline on purpose: the API-backed signals (CI status,
# branch protection) reflect live state, so an online badge changes when the repo
# has not — which is no good for a file that lives in git. Offline is
# reproducible, so the badge can be committed by the author and merely verified
# in CI, with nothing needing write access.
#
# `--out` leaves the file alone when the score is unchanged, so this and the
# pre-commit hook are both true no-ops on a clean tree.
.PHONY: badge
badge: build ## Refresh docs/badge.svg from this tree (offline, deterministic)
	$(BINARY) check . --offline --format svg --out docs/badge.svg

# The CI half of the same idea: verify, never write. Fails when the committed
# badge is stale, and says exactly how to fix it.
.PHONY: badge-check
badge-check: badge ## Fail when docs/badge.svg is out of date
	@git diff --quiet -- docs/badge.svg || { \
	  echo "docs/badge.svg is out of date. Run 'make badge' and commit the result"; \
	  echo "(or install the pre-commit hook so it happens for you)."; \
	  git --no-pager diff -- docs/badge.svg; \
	  exit 1; \
	}
	@echo "badge is current"
