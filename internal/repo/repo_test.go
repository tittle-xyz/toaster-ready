// SPDX-License-Identifier: Apache-2.0

package repo

import (
	"os"
	"path/filepath"
	"testing"
)

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
