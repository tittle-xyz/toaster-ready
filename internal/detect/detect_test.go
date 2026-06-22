// SPDX-License-Identifier: Apache-2.0

package detect

import (
	"os"
	"path/filepath"
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

func TestDetectGoAtRoot(t *testing.T) {
	res := Detect(tmpRepo(t, map[string]string{"go.mod": "module x\n"}))
	if res.Undetermined() {
		t.Fatal("expected a determination")
	}
	if !res.Has("go") {
		t.Fatalf("expected go, got %v", res.IDs())
	}
	if got := res.Stacks[0].Marker; got != "go.mod" {
		t.Fatalf("marker = %q, want go.mod", got)
	}
}

func TestDetectPolyglotNested(t *testing.T) {
	res := Detect(tmpRepo(t, map[string]string{
		"go.mod":             "module x\n",
		"web/package.json":   "{}",
		"infra/main.tf":      "",
		"api/Service.csproj": "<Project/>",
	}))
	for _, id := range []string{"go", "node", "terraform", "dotnet"} {
		if !res.Has(id) {
			t.Errorf("missing %s; got %v", id, res.IDs())
		}
	}
}

func TestShallowestMarkerWins(t *testing.T) {
	res := Detect(tmpRepo(t, map[string]string{
		"sub/dir/go.mod": "module deep\n",
		"go.mod":         "module root\n",
	}))
	if got := res.Stacks[0].Marker; got != "go.mod" {
		t.Fatalf("marker = %q, want root go.mod", got)
	}
}

func TestUndeterminedWhenNoMarkers(t *testing.T) {
	res := Detect(tmpRepo(t, map[string]string{"README.md": "# hi", "notes.txt": "x"}))
	if !res.Undetermined() {
		t.Fatalf("expected undetermined, got %v", res.IDs())
	}
}

func TestProfileForKnownAndFallback(t *testing.T) {
	if p := ProfileFor("go"); p.ID != "go" || len(p.LockFiles) == 0 {
		t.Fatalf("expected populated go profile, got %+v", p)
	}
	g := ProfileFor("cobol")
	if g.ID != "cobol" || len(g.LockFiles) != 0 || len(g.CoverageGlobs) != 0 {
		t.Fatalf("expected empty generic profile, got %+v", g)
	}
}
