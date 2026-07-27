APP  := toaster
MAIN := ./cmd/toaster

# `run` predates the shared build and is documented as the one-command demo, so
# keep this repo's version rather than the generic one.
SHARED_SKIP_TARGETS := run

SHARED_BUILD_REPO ?= https://github.com/tittle-xyz/go-shared-build.git
SHARED_BUILD_REF  ?= v0.8.1
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
