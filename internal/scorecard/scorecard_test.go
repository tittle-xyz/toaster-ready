// SPDX-License-Identifier: Apache-2.0

package scorecard

import (
	"math"
	"testing"
)

func TestBand(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0, "needs-work"}, {49.9, "needs-work"},
		{50, "functional"}, {84.9, "functional"},
		{85, "exemplary"}, {100, "exemplary"},
	}
	for _, c := range cases {
		if got := Band(c.score); got != c.want {
			t.Errorf("Band(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestDefaultWeightsSumTo100(t *testing.T) {
	if got := DefaultWeightSum(); got != 100 {
		t.Fatalf("default rubric weights sum to %v, want 100", got)
	}
}

func okSig() Evidence     { return Evidence{Signal: "x", Status: StatusOK, Found: Boolp(true)} }
func noDataSig() Evidence { return Evidence{Signal: "x", Status: StatusNoData, Reason: "blocked"} }

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAggregateAllScored(t *testing.T) {
	cats := []Category{
		{ID: "a", Weight: 60, Applicable: true, Normalized: 1, Signals: []Evidence{okSig()}},
		{ID: "b", Weight: 40, Applicable: true, Normalized: 0.5, Signals: []Evidence{okSig()}},
	}
	score, complete := Aggregate(cats)
	// 100 * (60*1 + 40*0.5) / 100 = 80
	if !approx(score, 80) {
		t.Fatalf("score = %v, want 80", score)
	}
	if !complete {
		t.Fatal("expected dataComplete")
	}
	if sum := cats[0].Contribution + cats[1].Contribution; !approx(sum, score) {
		t.Fatalf("contributions sum %v != score %v", sum, score)
	}
}

func TestNotApplicableRedistributes(t *testing.T) {
	cats := []Category{
		{ID: "a", Weight: 60, Applicable: true, Normalized: 1, Signals: []Evidence{okSig()}},
		{ID: "b", Weight: 40, Applicable: false, Normalized: 0, Signals: []Evidence{
			{Signal: "x", Status: StatusNotApplicable}}},
	}
	score, complete := Aggregate(cats)
	// b is N/A -> denominator is just a's 60; 100 * (60*1)/60 = 100
	if !approx(score, 100) {
		t.Fatalf("score = %v, want 100 (N/A weight redistributed)", score)
	}
	if !complete {
		t.Fatal("N/A should not mark the score incomplete")
	}
	if cats[1].Contribution != 0 {
		t.Fatalf("N/A category contributed %v, want 0", cats[1].Contribution)
	}
}

func TestFullyNoDataExcludedAndFlagged(t *testing.T) {
	cats := []Category{
		{ID: "a", Weight: 60, Applicable: true, Normalized: 1, Signals: []Evidence{okSig()}},
		{ID: "b", Weight: 40, Applicable: true, Normalized: 0, Signals: []Evidence{noDataSig()}},
	}
	score, complete := Aggregate(cats)
	// b is fully no-data -> excluded; 100 * (60*1)/60 = 100
	if !approx(score, 100) {
		t.Fatalf("score = %v, want 100 (fully-no-data excluded)", score)
	}
	if complete {
		t.Fatal("fully-no-data should flag the score as incomplete")
	}
	if len(cats[1].BlockedBy) == 0 {
		t.Fatal("fully-no-data category should report BlockedBy")
	}
}

func TestPartialNoDataStillCounts(t *testing.T) {
	cats := []Category{
		{ID: "a", Weight: 100, Applicable: true, Normalized: 0.5, Signals: []Evidence{okSig(), noDataSig()}},
	}
	score, complete := Aggregate(cats)
	if !approx(score, 50) {
		t.Fatalf("score = %v, want 50 (partial no-data still counts)", score)
	}
	if complete {
		t.Fatal("partial no-data should flag incomplete")
	}
}

func TestAllExcludedScoresZero(t *testing.T) {
	cats := []Category{
		{ID: "a", Weight: 50, Applicable: false, Signals: []Evidence{{Status: StatusNotApplicable}}},
		{ID: "b", Weight: 50, Applicable: true, Normalized: 0, Signals: []Evidence{noDataSig()}},
	}
	score, complete := Aggregate(cats)
	if score != 0 || complete {
		t.Fatalf("all-excluded => score 0, incomplete; got %v, %v", score, complete)
	}
}

func TestScoredAbsent(t *testing.T) {
	absent := Category{Applicable: true, Normalized: 0, Signals: []Evidence{
		{Status: StatusOK, Found: Boolp(false)}}}
	if !absent.ScoredAbsent() {
		t.Fatal("a determined zero should be ScoredAbsent")
	}
	blocked := Category{Applicable: true, Normalized: 0, Signals: []Evidence{noDataSig()}}
	if blocked.ScoredAbsent() {
		t.Fatal("a no-data zero must NOT be ScoredAbsent")
	}
}

func TestBoolp(t *testing.T) {
	if p := Boolp(true); p == nil || *p != true {
		t.Fatal("Boolp(true) should point to true")
	}
}
