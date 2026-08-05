// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The whole point of writeIfChanged is that re-running is a real no-op, so the
// pre-commit hook does not report "files were modified" for a change that is not
// there. Assert on mtime rather than content: identical content written twice
// would still churn the file, which is the failure we care about.
func TestWriteIfChangedLeavesIdenticalContentAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badge.svg")
	want := []byte("<svg>93</svg>")

	if err := writeIfChanged(path, want); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}

	// Backdate so an unwanted rewrite is unmistakable — same-second writes would
	// otherwise be indistinguishable from a no-op.
	old := first.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := writeIfChanged(path, want); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after second write: %v", err)
	}
	if !second.ModTime().Equal(old) {
		t.Errorf("identical content rewrote the file: mtime moved %v -> %v", old, second.ModTime())
	}
}

func TestWriteIfChangedWritesWhenContentDiffers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "badge.svg")
	if err := writeIfChanged(path, []byte("<svg>73</svg>")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeIfChanged(path, []byte("<svg>93</svg>")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "<svg>93</svg>" {
		t.Errorf("got %q, want the updated content", got)
	}
}

// The hook writes to docs/badge.svg in a fresh clone, where docs/ may not exist.
func TestWriteIfChangedCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs", "badge.svg")
	if err := writeIfChanged(path, []byte("<svg/>")); err != nil {
		t.Fatalf("write into a missing directory: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
