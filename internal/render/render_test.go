// SPDX-License-Identifier: Apache-2.0

package render

import (
	"strings"
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

func sample() scorecard.Scorecard {
	return scorecard.Scorecard{
		Repo: "owner/repo", Ref: "abc123", ScoredAt: "2026-01-01T00:00:00Z",
		Scorer: "toaster-ready 2.0 (deterministic)", Score: 72.5, Max: 100, Band: "functional",
		DataComplete: false, DetectedStack: []string{"go"},
		Categories: []scorecard.Category{
			{ID: "agent-instructions", Title: "Agent/human instructions", Weight: 15, Applicable: true, Normalized: 1, Contribution: 20.3, DataComplete: true},
			{ID: "cicd-pipeline", Title: "CI: test / build / deploy", Weight: 12, Applicable: true, Normalized: 0.5, Contribution: 8.1, DataComplete: false, BlockedBy: []string{"latest CI run green: no token"},
				Recommendations: []scorecard.Recommendation{{Category: "cicd-pipeline", Cause: scorecard.CauseNoData, Action: "Couldn't determine CI status; make it checkable."}}},
			{ID: "db-migrations", Title: "DB migrations", Weight: 3, Applicable: false},
		},
	}
}

func TestShields(t *testing.T) {
	for _, tc := range []struct {
		score float64
		band  string
		color string
	}{
		{40, "needs-work", "red"},
		{72, "functional", "yellow"},
		{90, "exemplary", "brightgreen"},
	} {
		out := Shields(scorecard.Scorecard{Score: tc.score})
		for _, want := range []string{
			`"schemaVersion":1`,
			`"label":"toaster-ready"`,
			tc.band,
			`"color":"` + tc.color + `"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("score %.0f: shields output missing %q\n%s", tc.score, want, out)
			}
		}
	}
}

func TestMarkdownContents(t *testing.T) {
	md := Markdown(sample())
	for _, want := range []string{
		"# toaster-ready scorecard — owner/repo",
		"Score: 72.5 / 100",
		"functional",
		"Agent/human instructions",
		"| Category | Weight | Score | Contribution | Notes |",
		"n/a",                  // the N/A category
		"Incomplete / blocked", // blocked section
		"latest CI run green: no token",
		"## Recommendations",
		"Couldn't determine CI status; make it checkable.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestHTMLContents(t *testing.T) {
	h := HTML(sample())
	for _, want := range []string{
		"<!doctype html>",
		"<title>toaster-ready scorecard — owner/repo</title>",
		"<table>",
		"Agent/human instructions",
		"Incomplete / blocked",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

func TestHTMLEscapesContent(t *testing.T) {
	sc := sample()
	sc.Repo = "owner/<script>"
	if h := HTML(sc); strings.Contains(h, "<script>") {
		t.Errorf("repo name not escaped in HTML:\n%s", h)
	}
}
