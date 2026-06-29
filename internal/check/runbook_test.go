// SPDX-License-Identifier: Apache-2.0

package check

import (
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

func TestRunnableRunbookDetectsRunCommands(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"npm run dev fenced", "## Run\n```sh\nnpm run dev\n```\n", true},
		{"docker compose up", "```\ndocker compose up\n```\n", true},
		{"go run cmd", "Run it:\n```sh\ngo run ./cmd/app\n```\n", true},
		{"flask run", "```\nflask run\n```\n", true},
		{"build is not run", "```sh\nmake build\nnpm test\n```\n", false},
		{"prose only", "Run the service after cloning it.\n", false},
		{"inline mention is not a runbook", "Start with `make run`.\n", false},   // not fenced
		{"rubric-table example", "| x | `docker compose up` example |\n", false}, // inline, not a runbook
		{"no code", "make run\n", false},                                         // bare prose
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "README.md", tc.body)
			_, _, ok := runnableRunbook(openLocal(t, dir))
			if ok != tc.want {
				t.Errorf("runnableRunbook(%q) = %v, want %v", tc.body, ok, tc.want)
			}
		})
	}
}

func TestRunnableRunbookReportsEndpoint(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "CLAUDE.md", "```sh\nnpm run dev   # serves on http://localhost:3000\n```\n")
	src, endpoint, ok := runnableRunbook(openLocal(t, dir))
	if !ok || !endpoint {
		t.Fatalf("want ok+endpoint, got ok=%v endpoint=%v", ok, endpoint)
	}
	if src != "CLAUDE.md" {
		t.Errorf("src = %q, want CLAUDE.md (agent instructions preferred)", src)
	}
}

// Structural completeness alone can't reach full marks; the runnable runbook is
// the lever to 1.0.
func TestSetupRunbookIsTheLeverToFullMarks(t *testing.T) {
	const goMod, makefile = "module x\n", "run:\n\techo run\nbuild:\n\techo build\n"

	// Structurally complete (runner + manifest) but no documented run command.
	noRunbook := scoreDir(t, writeRepo(t, map[string]string{"go.mod": goMod, "Makefile": makefile}))
	got := cat(noRunbook, scorecard.CatSetup).Normalized
	if got != setupStructuralComplete {
		t.Errorf("structure-only setup = %v, want %v (no full marks without a runbook)", got, setupStructuralComplete)
	}

	// Same structure plus a copy-pasteable run command in the README.
	withRunbook := scoreDir(t, writeRepo(t, map[string]string{
		"go.mod": goMod, "Makefile": makefile, "README.md": "## Run\n```sh\nmake run\n```\n",
	}))
	if got := cat(withRunbook, scorecard.CatSetup).Normalized; got != 1 {
		t.Errorf("structure + runbook setup = %v, want 1.0", got)
	}
}

// A bare run command with no structural signals shouldn't masquerade as a
// reproducible setup — it lifts, but off a zero floor.
func TestSetupRunbookWithoutStructureIsPartial(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"README.md": "```sh\ndocker compose up\n```\n",
	}))
	got := cat(sc, scorecard.CatSetup).Normalized
	if got != setupRunbookBonus {
		t.Errorf("runbook-only setup = %v, want %v", got, setupRunbookBonus)
	}
}
