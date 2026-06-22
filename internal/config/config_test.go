// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultMatchesRubric(t *testing.T) {
	d := Default()
	if d.Weights["agent-instructions"] != 15 {
		t.Errorf("default agent-instructions weight = %v, want 15", d.Weights["agent-instructions"])
	}
	if d.Gate.Threshold != 50 {
		t.Errorf("default gate threshold = %v, want 50", d.Gate.Threshold)
	}
	if d.ContextBudget.Soft == 0 || d.ContextBudget.Hard == 0 {
		t.Errorf("default context budget unset: %+v", d.ContextBudget)
	}
	if d.Recommend.Below != 0.75 {
		t.Errorf("default recommend.below = %v, want 0.75", d.Recommend.Below)
	}
}

func TestExplicitZeroThresholdHonored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".toaster-ready.yml"), []byte("gate:\n  threshold: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gate.Threshold != 0 {
		t.Errorf("explicit threshold 0 should be honored, got %v", cfg.Gate.Threshold)
	}
}

func TestBareBlockKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".toaster-ready.yml"), []byte("gate: {}\nrecommend: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gate.Threshold != 50 || cfg.Recommend.Below != 0.75 {
		t.Errorf("bare blocks should keep defaults, got gate=%v recommend=%v", cfg.Gate.Threshold, cfg.Recommend.Below)
	}
}

func TestLoadNoFileReturnsDefaults(t *testing.T) {
	cfg, path, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("path = %q, want empty (no file)", path)
	}
	if cfg.Gate.Threshold != 50 {
		t.Errorf("expected default threshold, got %v", cfg.Gate.Threshold)
	}
}

func TestLoadOverrideMerges(t *testing.T) {
	dir := t.TempDir()
	yml := "" +
		"weights:\n" +
		"  agent-instructions: 30\n" +
		"disabled:\n" +
		"  - db-migrations\n" +
		"gate:\n" +
		"  threshold: 70\n" +
		"contextBudget:\n" +
		"  soft: 1000\n"
	if err := os.WriteFile(filepath.Join(dir, ".toaster-ready.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, path, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected the .toaster-ready.yml to be discovered")
	}
	if cfg.Weights["agent-instructions"] != 30 {
		t.Errorf("overridden weight = %v, want 30", cfg.Weights["agent-instructions"])
	}
	if cfg.Weights["setup-reproducibility"] != 12 {
		t.Errorf("untouched weight changed: %v, want 12 (default)", cfg.Weights["setup-reproducibility"])
	}
	if !cfg.IsDisabled("db-migrations") {
		t.Error("db-migrations should be disabled")
	}
	if cfg.Gate.Threshold != 70 {
		t.Errorf("gate threshold = %v, want 70", cfg.Gate.Threshold)
	}
	if cfg.ContextBudget.Soft != 1000 {
		t.Errorf("context soft = %v, want 1000", cfg.ContextBudget.Soft)
	}
	if cfg.ContextBudget.Hard == 0 {
		t.Error("context hard should keep its default, not reset to 0")
	}
}

func TestLoadUnknownCategoryErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".toaster-ready.yml"), []byte("weights:\n  not-a-category: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir, ""); err == nil {
		t.Fatal("expected an error for an unknown category id")
	}
}

func TestExplicitMissingPathErrors(t *testing.T) {
	if _, _, err := Load(t.TempDir(), filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("expected an error for a missing explicit config path")
	}
}
