// SPDX-License-Identifier: Apache-2.0

package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/config"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

// codeText must see commands in fenced blocks and inline spans, but never prose
// — so "make sure" / "just works" can't masquerade as invocations.
func TestCodeTextIgnoresProse(t *testing.T) {
	md := "Be sure to make sure it works, it just works.\n\n" +
		"```sh\nmake build\nnpm run dev\n```\n\nRun `just test` to test.\n"
	code := codeText(md)
	for _, want := range []string{"make build", "npm run dev", "just test"} {
		if !strings.Contains(code, want) {
			t.Errorf("codeText missing %q; got:\n%s", want, code)
		}
	}
	if strings.Contains(code, "make sure") || strings.Contains(code, "just works") {
		t.Errorf("codeText leaked prose:\n%s", code)
	}
}

func TestCommandDriftFlagsMissingTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", "build:\n\tgo build ./...\ntest:\n\tgo test ./...\n")
	write(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
	write(t, dir, "CLAUDE.md",
		"## Run\n```sh\nmake build\nmake deploy\nnpm run dev\nnpm run start\n```\n"+
			"Also `make test`. And remember to make sure things work.\n")

	r := openLocal(t, dir)
	hits := commandDrift(r, mustRead(t, r, "CLAUDE.md"))

	want := map[string]bool{"make deploy": true, "npm run start": true}
	for _, h := range hits {
		if !want[h] {
			t.Errorf("unexpected drift hit %q (false positive)", h)
		}
		delete(want, h)
	}
	for missing := range want {
		t.Errorf("expected drift hit %q, not found in %v", missing, hits)
	}
}

// A clean repo (all documented targets exist, instructions fresh, git history
// present) must report freshness+drift signals as Found=true and keep full marks.
func TestInstructionsCleanRepoNoDrift(t *testing.T) {
	dir := newGitRepo(t)
	write(t, dir, "Makefile", "build:\n\tgo build ./...\n")
	write(t, dir, "main.go", "package main\nfunc main() {}\n")
	commit(t, dir, "feat: initial")
	write(t, dir, "CLAUDE.md", longBody("Build with `make build`."))
	commit(t, dir, "docs: instructions")

	c := agentInstructions(openLocal(t, dir), config.Default().ContextBudget)
	if got := found(t, c, driftSignal); !got {
		t.Error("command-drift signal should be Found=true (no drift)")
	}
	if got := found(t, c, staleSignal); !got {
		t.Error("freshness signal should be Found=true (fresh)")
	}
	if c.Normalized != 1 {
		t.Errorf("clean repo normalized = %v, want 1", c.Normalized)
	}
}

// Instructions committed before a long run of source commits are stale: the
// freshness signal is Found=false and the subscore is penalized.
func TestInstructionsStaleVsChurn(t *testing.T) {
	dir := newGitRepo(t)
	write(t, dir, "CLAUDE.md", longBody("No commands here."))
	commit(t, dir, "docs: instructions") // markdown-only: 0 source commits

	for i := 0; i < 10; i++ { // 10 source commits all postdate the docs
		write(t, dir, "main.go", "package main\n// rev "+strconv.Itoa(i)+"\nfunc main() {}\n")
		commit(t, dir, "feat: change "+strconv.Itoa(i))
	}

	c := agentInstructions(openLocal(t, dir), config.Default().ContextBudget)
	if found(t, c, staleSignal) {
		t.Error("freshness signal should be Found=false (stale)")
	}
	if c.Normalized >= 1 {
		t.Errorf("stale repo normalized = %v, want < 1", c.Normalized)
	}
}

// The governing rule: no git history => no-data, never a penalty.
func TestInstructionsStalenessNoDataWithoutGit(t *testing.T) {
	dir := t.TempDir() // not a git repo
	write(t, dir, "CLAUDE.md", longBody("Hello."))

	c := agentInstructions(openLocal(t, dir), config.Default().ContextBudget)
	var sawNoData bool
	for _, e := range c.Signals {
		if e.Signal == staleSignal {
			if e.Status != scorecard.StatusNoData {
				t.Errorf("freshness status = %q, want no-data without git", e.Status)
			}
			sawNoData = true
		}
	}
	if !sawNoData {
		t.Fatal("expected a no-data freshness signal")
	}
	// no-data must not drag the score down on its own (drift signal is clean here).
	if c.Normalized != 1 {
		t.Errorf("no-data freshness penalized the score: normalized = %v, want 1", c.Normalized)
	}
}

// --- helpers ---------------------------------------------------------------

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openLocal(t *testing.T, dir string) *repo.Repo {
	t.Helper()
	r, err := repo.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func mustRead(t *testing.T, r *repo.Repo, rel string) string {
	t.Helper()
	body, err := r.Read(rel)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func found(t *testing.T, c scorecard.Category, signal string) bool {
	t.Helper()
	for _, e := range c.Signals {
		if e.Signal == signal && e.Status == scorecard.StatusOK && e.Found != nil {
			return *e.Found
		}
	}
	t.Fatalf("ok signal %q not found", signal)
	return false
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	return dir
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-q", "-m", msg)
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// longBody pads s past the 800-byte substance floor so base=1.0 and the drift
// multiplier is what moves the subscore.
func longBody(s string) string {
	pad := "\n<!-- " + strings.Repeat("padding ", 120) + "-->\n"
	return "# Instructions\n\n" + s + pad
}

// --- shared-build include tests -------------------------------------------

// A Makefile that delegates to a fetched shared build file via `include` must
// not report drift for targets defined in the included file. This is the
// go-shared-build pattern: `make check`, `make run`, `make docker-build` are
// defined in .build/go.mk which is fetched at runtime and gitignored.
func TestCommandDriftFollowsInclude(t *testing.T) {
	dir := t.TempDir()
	// Makefile pins a shared build and includes it. The shared file defines
	// `check`, `run`, and `docker-build` — none of which appear in the Makefile
	// itself.
	write(t, dir, "Makefile",
		"APP := myapp\n"+
			"SHARED_BUILD_DIR ?= .build\n"+
			"_ := $(shell test -f $(SHARED_BUILD_DIR)/go.mk || echo fetch)\n"+
			"include $(SHARED_BUILD_DIR)/go.mk\n"+
			"include $(SHARED_BUILD_DIR)/docker.mk\n")
	// Simulate the fetched shared build file in .build/.
	write(t, dir, ".build/go.mk",
		"check:\n\tgo vet ./... && go test ./...\n"+
			"run:\n\tgo run ./cmd/app\n"+
			"build:\n\tgo build ./...\n")
	write(t, dir, ".build/docker.mk",
		"docker-build:\n\tdocker build .\n"+
			"docker-run:\n\tdocker run .\n")
	write(t, dir, "CLAUDE.md",
		longBody("Run `make check`. Build with `make build`. Start with `make run`. Container: `make docker-build`."))

	r := openLocal(t, dir)
	hits := commandDrift(r, mustRead(t, r, "CLAUDE.md"))

	if len(hits) > 0 {
		t.Errorf("expected no drift hits for shared-build targets, got: %v", hits)
	}
}

// Targets NOT defined in any included file are still reported as drift.
func TestCommandDriftStillCatchesMissingTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile",
		"SHARED_BUILD_DIR ?= .build\n"+
			"include $(SHARED_BUILD_DIR)/go.mk\n")
	write(t, dir, ".build/go.mk", "check:\n\tgo test ./...\n")
	write(t, dir, "CLAUDE.md",
		longBody("Run `make check` and then `make deploy`."))

	r := openLocal(t, dir)
	hits := commandDrift(r, mustRead(t, r, "CLAUDE.md"))

	if len(hits) != 1 || hits[0] != "make deploy" {
		t.Errorf("expected [make deploy], got %v", hits)
	}
}

// Variable expansion with ?= defaults must resolve correctly.
func TestCommandDriftExpandsDefaultVars(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile",
		"SHARED ?= .build\n"+
			"include $(SHARED)/go.mk\n")
	write(t, dir, ".build/go.mk", "lint:\n\tgolangci-lint run\n")
	write(t, dir, "CLAUDE.md", longBody("Lint with `make lint`."))

	r := openLocal(t, dir)
	hits := commandDrift(r, mustRead(t, r, "CLAUDE.md"))
	if len(hits) != 0 {
		t.Errorf("expected no drift hits, got %v", hits)
	}
}

// --- bare Jira key tests ---------------------------------------------------

// Bare PROJECT-NNN keys are the idiomatic internal cross-reference style and
// must be counted as source-material links.
func TestCountLinksDetectsBareJiraKeys(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want int
	}{
		{
			name: "bare key in text",
			md:   "See DM-6507 for background.",
			want: 1,
		},
		{
			name: "bare key in backtick",
			md:   "Closes `DM-1234`.",
			want: 1,
		},
		{
			name: "multiple bare keys",
			md:   "DM-6507 and DM-7249 are related.",
			want: 2,
		},
		{
			name: "full atlassian URL still counts",
			md:   "See https://evertune.atlassian.net/browse/DM-6507",
			want: 1,
		},
		{
			name: "no false positive on single-letter prefix",
			md:   "See A-1 or I-42.",
			want: 0,
		},
		{
			name: "no false positive on version numbers",
			md:   "Uses Go 1.25 and version V2-3 is not a Jira key.",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countLinks(tc.md)
			if got != tc.want {
				t.Errorf("countLinks(%q) = %d, want %d", tc.md, got, tc.want)
			}
		})
	}
}
