// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
