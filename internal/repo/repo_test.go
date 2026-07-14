// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		git(t, dir, args...)
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Files() must not descend into dependency/vendor dirs — otherwise the secret
// scan flags credentials inside installed third-party packages (issue #12).
func TestFilesSkipsDependencyDirs(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "app.py", "x = 1\n")
	mustWrite(t, dir, ".venv/lib/python3.11/site-packages/pkg/config.py",
		"password = \"hunter2hunter2\"\n")
	mustWrite(t, dir, "node_modules/pkg/index.js", "const secret = 'hunter2hunter2'\n")

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var sawApp bool
	for _, f := range r.Files() {
		if strings.HasPrefix(f, ".venv"+string(filepath.Separator)) ||
			strings.HasPrefix(f, "node_modules"+string(filepath.Separator)) {
			t.Errorf("Files() must skip dependency dirs, but returned %q", f)
		}
		if f == "app.py" {
			sawApp = true
		}
	}
	if !sawApp {
		t.Error("Files() should still include the repo's own source (app.py)")
	}
}

// A gitignored file isn't part of the repo — a fresh clone doesn't have it — so
// the walk must not surface it. Otherwise a developer's local .env scores
// differently than the same commit does in CI (issue #22).
func TestFilesSkipsGitignored(t *testing.T) {
	dir := newGitRepo(t)
	mustWrite(t, dir, ".gitignore", ".env\ndata/\n")
	mustWrite(t, dir, "app.py", "x = 1\n")
	mustWrite(t, dir, ".env", "password = \"hunter2hunter2\"\n")
	mustWrite(t, dir, "data/dump.sql", "-- big\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	files := r.Files()
	for _, unwanted := range []string{".env", filepath.Join("data", "dump.sql")} {
		if slices.Contains(files, unwanted) {
			t.Errorf("Files() must skip gitignored %q, got %v", unwanted, files)
		}
	}
	for _, want := range []string{"app.py", ".gitignore"} {
		if !slices.Contains(files, want) {
			t.Errorf("Files() should still include %q, got %v", want, files)
		}
	}
}

// The mirror of the rule above: a file that matches an ignore pattern but was
// committed anyway IS in the repo, so it must still be walked (and its secrets
// flagged). git status is index-aware, which is what buys us this for free.
func TestFilesIncludesTrackedFileMatchingIgnorePattern(t *testing.T) {
	dir := newGitRepo(t)
	mustWrite(t, dir, ".gitignore", ".env\n")
	mustWrite(t, dir, ".env", "password = \"hunter2hunter2\"\n")
	git(t, dir, "add", "-f", ".gitignore", ".env")
	git(t, dir, "commit", "-q", "-m", "init")

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if !slices.Contains(r.Files(), ".env") {
		t.Errorf("a committed .env is in the repo and must be walked, got %v", r.Files())
	}
}

// Scoring a subdirectory of a git repo: git reports ignored paths relative to
// the *repository* root ("svc/.env"), while the walk sees them relative to the
// scored root (".env"). Without reconciling the two, nothing would ever match.
func TestFilesSkipsGitignoredWhenRootIsSubdirectory(t *testing.T) {
	dir := newGitRepo(t)
	mustWrite(t, dir, ".gitignore", "svc/.env\n")
	mustWrite(t, dir, "svc/app.py", "x = 1\n")
	mustWrite(t, dir, "svc/.env", "password = \"hunter2hunter2\"\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")

	r, err := Open(filepath.Join(dir, "svc"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	files := r.Files()
	if slices.Contains(files, ".env") {
		t.Errorf("Files() must skip gitignored svc/.env when scoring svc/, got %v", files)
	}
	if !slices.Contains(files, "app.py") {
		t.Errorf("Files() should still include app.py, got %v", files)
	}
}

// Without git there's nothing to ask about ignore rules, so the walk falls back
// to showing everything — the behavior that predates ignore-awareness.
func TestFilesWithoutGitIgnoresNothing(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, ".gitignore", ".env\n")
	mustWrite(t, dir, ".env", "password = \"hunter2hunter2\"\n")

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if !slices.Contains(r.Files(), ".env") {
		t.Errorf("non-git tree: expected no ignore filtering, got %v", r.Files())
	}
}

func TestReadAndExistsRefuseTraversal(t *testing.T) {
	dir := t.TempDir()
	// a secret outside the repo root
	outside := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	if err := os.WriteFile(outside, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.WriteFile(filepath.Join(dir, "inside.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// In-tree access still works.
	if !r.Exists("inside.txt") {
		t.Fatal("expected inside.txt to exist")
	}
	if body, err := r.Read("inside.txt"); err != nil || body != "ok" {
		t.Fatalf("read inside.txt: %q, %v", body, err)
	}

	// Traversal is refused.
	if r.Exists("../outside-secret.txt") {
		t.Error("Exists must refuse a path escaping the root")
	}
	if _, err := r.Read("../outside-secret.txt"); err == nil {
		t.Error("Read must refuse a path escaping the root")
	}
}
