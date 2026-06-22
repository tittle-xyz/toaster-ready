// SPDX-License-Identifier: Apache-2.0

package ctxbudget

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/repo"
)

func tmpRepo(t *testing.T, files map[string]string) *repo.Repo {
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
	r, err := repo.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestTokensFromBytes(t *testing.T) {
	if got := TokensFromBytes(4); got != 1 {
		t.Fatalf("TokensFromBytes(4) = %d, want 1", got)
	}
	if got := TokensFromBytes(0); got != 0 {
		t.Fatalf("TokensFromBytes(0) = %d, want 0", got)
	}
}

func TestWithinBudget(t *testing.T) {
	est := Compute(tmpRepo(t, map[string]string{"CLAUDE.md": "be concise\n"}))
	if est.Status() != Within {
		t.Fatalf("status = %q, want within", est.Status())
	}
	if est.AlwaysLoadedTokens == 0 || len(est.Files) != 1 {
		t.Fatalf("expected one counted file with tokens, got %+v", est)
	}
}

func TestOverHardBudget(t *testing.T) {
	big := strings.Repeat("x", (HardBudgetTokens+1000)*4)
	est := Compute(tmpRepo(t, map[string]string{"CLAUDE.md": big}))
	if est.Status() != OverHard {
		t.Fatalf("status = %q, want over-hard (tokens=%d)", est.Status(), est.AlwaysLoadedTokens)
	}
}

func TestMemoryIndexCounted(t *testing.T) {
	est := Compute(tmpRepo(t, map[string]string{
		"AGENTS.md": "x",
		"MEMORY.md": "memory index entries",
	}))
	var kinds []string
	for _, f := range est.Files {
		kinds = append(kinds, f.Kind)
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "memory-index") {
		t.Fatalf("memory index not counted; kinds=%s", joined)
	}
}

func TestImportsResolvedOneLevel(t *testing.T) {
	est := Compute(tmpRepo(t, map[string]string{
		"CLAUDE.md":            "See @./docs/conventions.md for rules\n",
		"docs/conventions.md":  "the conventions, always loaded",
		"docs/unreferenced.md": "should NOT be counted",
	}))
	var imported bool
	for _, f := range est.Files {
		if f.Path == "docs/conventions.md" && f.Kind == "import" {
			imported = true
		}
		if f.Path == "docs/unreferenced.md" {
			t.Fatal("unreferenced file must not be counted")
		}
	}
	if !imported {
		t.Fatalf("imported file not counted; files=%+v", est.Files)
	}
}

func TestNoAgentContext(t *testing.T) {
	est := Compute(tmpRepo(t, map[string]string{"README.md": "# hi"}))
	if est.AlwaysLoadedTokens != 0 || len(est.Files) != 0 {
		t.Fatalf("expected empty estimate, got %+v", est)
	}
	if est.Status() != Within {
		t.Fatalf("empty status = %q, want within", est.Status())
	}
}
