// SPDX-License-Identifier: Apache-2.0

package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/config"
	"github.com/tittle-xyz/toaster-ready/internal/githubclient"
	"github.com/tittle-xyz/toaster-ready/internal/repo"
	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

const fixedScoredAt = "2026-01-01T00:00:00Z"

func scoreDir(t *testing.T, dir string) scorecard.Scorecard {
	t.Helper()
	return scoreDirCfg(t, dir, config.Default())
}

func scoreDirCfg(t *testing.T, dir string, cfg config.Config) scorecard.Scorecard {
	t.Helper()
	r, err := repo.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return Run(r, githubclient.NewStub(), fixedScoredAt, cfg)
}

func cat(sc scorecard.Scorecard, id string) scorecard.Category {
	for _, c := range sc.Categories {
		if c.ID == id {
			return c
		}
	}
	return scorecard.Category{}
}

// The bad-repo fixture should land in needs-work, trip the gate's essentials
// floor on all three critical categories, and surface the secret floor.
func TestBadRepoFixture(t *testing.T) {
	sc := scoreDir(t, filepath.Join("testdata", "badrepo"))

	if sc.Band != "needs-work" {
		t.Errorf("band = %q (score %.1f), want needs-work", sc.Band, sc.Score)
	}
	for _, id := range []string{scorecard.CatAgentInstructions, scorecard.CatPurpose, scorecard.CatCICD} {
		if !cat(sc, id).ScoredAbsent() {
			t.Errorf("%s: expected ScoredAbsent (gate floor) in bad repo", id)
		}
	}
	var sawSecret bool
	for _, e := range cat(sc, scorecard.CatConfigSecrets).Signals {
		if e.Signal == "hardcoded secret" {
			sawSecret = true
		}
	}
	if !sawSecret {
		t.Error("expected the secret floor to trip on the bad repo")
	}
}

// Below-threshold categories get recommendations keyed by cause; full-marks
// categories get none.
func TestRecommendationsByCause(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"README.md":                "# Widget\n\n## What\nA service.\n\n## Setup\nRun it. " + strings.Repeat("Orientation. ", 30),
		"CLAUDE.md":                "# Agent guide\n\n" + strings.Repeat("Use the Makefile; tests via make test. ", 30),
		"go.mod":                   "module example.com/widget\n",
		"Makefile":                 "run:\n\techo run\n",
		".github/workflows/ci.yml": "name: ci\non: [push]\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make test\n",
	})
	sc := scoreDir(t, dir)

	// No tests -> testing-and-coverage gets a "miss" recommendation.
	testing := cat(sc, scorecard.CatTesting)
	if len(testing.Recommendations) == 0 || testing.Recommendations[0].Cause != scorecard.CauseMiss {
		t.Errorf("testing should have a miss recommendation, got %+v", testing.Recommendations)
	}
	// Offline CI-green is no-data -> cicd gets a "no-data" recommendation.
	cicd := cat(sc, scorecard.CatCICD)
	var sawNoData bool
	for _, r := range cicd.Recommendations {
		if r.Cause == scorecard.CauseNoData {
			sawNoData = true
		}
	}
	if !sawNoData {
		t.Errorf("cicd should have a no-data recommendation, got %+v", cicd.Recommendations)
	}
	// agent-instructions is full marks here -> no recommendation.
	if r := cat(sc, scorecard.CatAgentInstructions).Recommendations; len(r) != 0 {
		t.Errorf("full-marks agent-instructions should have no recommendations, got %+v", r)
	}
}

// A present-but-bloated CLAUDE.md should get a "trim it" recommendation, never
// "add a CLAUDE.md" (the file exists).
func TestOverBudgetRecommendationIsTrimNotAdd(t *testing.T) {
	huge := "# Agent guide\n\n" + strings.Repeat("x", 80*1024) // well over the hard budget
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"CLAUDE.md": huge,
		"go.mod":    "module x\n",
	}))
	ai := cat(sc, scorecard.CatAgentInstructions)
	if len(ai.Recommendations) == 0 {
		t.Fatal("over-budget agent-instructions should have a recommendation")
	}
	for _, r := range ai.Recommendations {
		if strings.Contains(strings.ToLower(r.Action), "add a claude.md") {
			t.Errorf("contradictory advice: %q", r.Action)
		}
		if r.Cause != scorecard.CauseImprove {
			t.Errorf("over-budget should be an improve recommendation, got cause %q", r.Cause)
		}
	}
}

func hasFailureContaining(failures []string, substr string) bool {
	for _, f := range failures {
		if strings.Contains(f, substr) {
			return true
		}
	}
	return false
}

// The bad repo fails the gate on the essentials floor, the secret scan, and the
// score threshold; a passing threshold of 0 still fails it on the floor + secret.
func TestGateBadRepoFails(t *testing.T) {
	sc := scoreDir(t, filepath.Join("testdata", "badrepo"))
	f := GateFailures(sc, 50)
	if len(f) == 0 {
		t.Fatal("bad repo should fail the gate")
	}
	if !hasFailureContaining(f, "agent-instructions") || !hasFailureContaining(f, "hardcoded secrets") || !hasFailureContaining(f, "below threshold") {
		t.Errorf("expected floor + secret + threshold failures, got %v", f)
	}
	if floor := GateFailures(sc, 0); !hasFailureContaining(floor, "hardcoded secrets") || hasFailureContaining(floor, "below threshold") {
		t.Errorf("with threshold 0, expect floor/secret but no threshold failure, got %v", floor)
	}
}

// A well-formed repo passes at the baseline but fails when the threshold is set
// above its score.
func TestGateThresholdGatesGoodRepo(t *testing.T) {
	dir := writeRepo(t, map[string]string{
		"README.md":                "# Widget\n\n## What\nA service.\n\n## Setup\nRun `make run`. " + strings.Repeat("Orientation. ", 30),
		"CLAUDE.md":                "# Agent guide\n\n" + strings.Repeat("Use the Makefile; tests via make test. ", 20),
		"go.mod":                   "module example.com/widget\n",
		"Makefile":                 "run:\n\techo run\n",
		".github/workflows/ci.yml": "name: ci\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make test\n",
		".env.example":             "API_URL=\n",
		".editorconfig":            "root = true\n",
		"docs/adr/0001-init.md":    "# ADR 1\nWhy.\n",
	})
	sc := scoreDir(t, dir)

	if f := GateFailures(sc, 50); len(f) != 0 {
		t.Errorf("good repo should pass at threshold 50, got %v (score %.1f)", f, sc.Score)
	}
	if f := GateFailures(sc, 99); !hasFailureContaining(f, "below threshold") {
		t.Errorf("good repo should fail at threshold 99, got %v (score %.1f)", f, sc.Score)
	}
}

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// db-migrations is not-applicable when no database is detected — its weight
// redistributes rather than scoring the repo zero.
func TestDBMigrationsNotApplicableNoDB(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{"go.mod": "module x\n"}))
	if cat(sc, scorecard.CatDBMigrations).Applicable {
		t.Fatal("no DB => db-migrations should be not-applicable")
	}
}

func TestDBMigrationsScoredWhenDBPresent(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"composer.json":           `{"require":{"doctrine/orm":"^2.0"}}`,
		"migrations/Version1.php": "<?php // migration",
	}))
	m := cat(sc, scorecard.CatDBMigrations)
	if !m.Applicable {
		t.Fatal("a DB driver should make db-migrations applicable")
	}
	if m.Normalized != 1.0 {
		t.Fatalf("DB + migrations => normalized 1.0, got %v", m.Normalized)
	}
}

func TestDependencyPatchingNotApplicableNoEcosystem(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{"README.md": "# docs only"}))
	if cat(sc, scorecard.CatDependencyPatching).Applicable {
		t.Fatal("no manifest/lockfile/stack => dependency-patching should be not-applicable")
	}
}

// A repo whose run path is `npm run dev` (a package.json scripts map, not a
// Makefile) must still register the setup task-runner signal — that was a false
// negative that under-credited script-runner repos (issue #5).
func TestSetupTaskRunnerRecognizesScriptRunners(t *testing.T) {
	with := cat(scoreDir(t, writeRepo(t, map[string]string{
		"package.json": `{"name":"app","scripts":{"dev":"next dev","build":"next build"}}`,
	})), scorecard.CatSetup)
	if !signalTrue(with, "task runner") {
		t.Fatal("package.json scripts should satisfy the setup task-runner signal")
	}

	// Control: a package.json with no scripts is not a task runner.
	without := cat(scoreDir(t, writeRepo(t, map[string]string{
		"package.json": `{"name":"app","dependencies":{"left-pad":"1.0.0"}}`,
	})), scorecard.CatSetup)
	if signalTrue(without, "task runner") {
		t.Fatal("a scripts-less package.json should not register a task runner")
	}
}

func signalTrue(c scorecard.Category, name string) bool {
	for _, s := range c.Signals {
		if s.Signal == name {
			return s.Found != nil && *s.Found
		}
	}
	return false
}

// A minimal but well-formed repo should clear the functional floor and keep all
// gate-critical categories present.
func TestGoodRepoScoresFunctional(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Widget\n\n## What\nA widget service.\n\n## Setup\nRun `make run` after cloning. "+strings.Repeat("Orientation and background. ", 30))
	write("CLAUDE.md", "# Agent guide\n\n"+strings.Repeat("Use the Makefile targets; tests via `make test`; deploy via CI. ", 20))
	write("go.mod", "module example.com/widget\n")
	write("Makefile", "run:\n\techo run\ntest:\n\techo test\n")
	write(".github/workflows/ci.yml", "name: ci\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make test\n")
	write(".env.example", "API_URL=\n")
	write(".editorconfig", "root = true\n")
	write("docs/adr/0001-init.md", "# ADR 1\nWhy we built it.\n")

	sc := scoreDir(t, dir)

	if sc.Band == "needs-work" {
		t.Errorf("good repo scored needs-work (%.1f); want functional or better", sc.Score)
	}
	for _, id := range []string{scorecard.CatAgentInstructions, scorecard.CatPurpose, scorecard.CatCICD} {
		if cat(sc, id).ScoredAbsent() {
			t.Errorf("%s should be present in the good repo", id)
		}
	}
}
