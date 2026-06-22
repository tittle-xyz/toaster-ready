// SPDX-License-Identifier: Apache-2.0

// Package scorecard defines the output schema toaster-ready emits for a scored repo
// (rubric v2 — see docs/adr/0002).
//
// Two design rules carry over and extend from v0.1:
//
//   - Three-state-plus signal: a check is a real determination (ok — found or
//     absent), one that could not be made (no-data, with a reason), or one that
//     does not apply to this repo (not-applicable). no-data is NEVER collapsed
//     into a score of 0.
//   - Weighted categories scored out of 100. Each category yields a normalized
//     subscore in [0,1]; its contribution is its weight times that subscore.
//     Not-applicable and fully-no-data categories are dropped and their weight
//     redistributed across the scored categories, so the /100 stays fair across
//     repo types (a DB-less repo isn't punished for having no migrations).
package scorecard

// Status is the outcome of a single signal.
type Status string

const (
	// StatusOK means the signal was determined — see Found for the result.
	StatusOK Status = "ok"
	// StatusNoData means the signal could not be determined — see Reason.
	StatusNoData Status = "no-data"
	// StatusNotApplicable means the signal/category does not apply to this repo.
	StatusNotApplicable Status = "not-applicable"
)

// Method records how a signal was gathered.
const (
	MethodFile    = "file"    // presence/shape of a file on disk
	MethodContent = "content" // reading file contents
	MethodGit     = "git"     // local git facts (tags, sha)
	MethodAPI     = "api"     // GitHub API (may be no-data without a token/permission)
	MethodSkill   = "skill"   // resolved by the skill+MCP layer (out of binary scope)
)

// Evidence is one signal feeding a category's score. Every score traces to one
// or more of these — the provenance toaster-ready guarantees.
type Evidence struct {
	Signal  string         `json:"signal"`
	Method  string         `json:"method"`
	Status  Status         `json:"status"`
	Found   *bool          `json:"found,omitempty"`  // meaningful only when Status==ok
	Path    string         `json:"path,omitempty"`   // relative path the signal came from
	Ref     string         `json:"ref,omitempty"`    // line range / locator within Path
	Source  string         `json:"source,omitempty"` // filesystem | github-api | skill+mcp
	Reason  string         `json:"reason,omitempty"` // why, when Status==no-data
	Note    string         `json:"note,omitempty"`
	Metrics map[string]any `json:"metrics,omitempty"` // numeric detail (e.g. context-budget tokens)
}

// RecCause classifies why a category scored low.
type RecCause string

const (
	CauseMiss    RecCause = "miss"    // a real determination that something is absent
	CauseNoData  RecCause = "no-data" // we could not check — make it checkable
	CauseImprove RecCause = "improve" // present but partial — strengthen it
)

// Recommendation is actionable guidance attached to a low-scoring category.
type Recommendation struct {
	Category    string   `json:"category"`
	Cause       RecCause `json:"cause"`
	Action      string   `json:"action"`
	EvidenceRef string   `json:"evidenceRef,omitempty"`
}

// Category is one weighted rubric axis. The deterministic pass sets Normalized
// in [0,1]; for categories marked Judgment a later (optional, off-CI) pass may
// refine it. Contribution is filled by Aggregate: the category's renormalized
// share of the /100 total (0 when the category is excluded).
type Category struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Weight          float64          `json:"weight"`
	Applicable      bool             `json:"applicable"`
	Normalized      float64          `json:"normalized"`
	Contribution    float64          `json:"contribution"`
	Judgment        bool             `json:"judgment,omitempty"`
	DataComplete    bool             `json:"dataComplete"`
	BlockedBy       []string         `json:"blockedBy,omitempty"`
	Signals         []Evidence       `json:"signals"`
	Recommendations []Recommendation `json:"recommendations,omitempty"`
	Rationale       string           `json:"rationale,omitempty"`
}

func (c Category) hasNoData() bool {
	for _, e := range c.Signals {
		if e.Status == StatusNoData {
			return true
		}
	}
	return false
}

// determinable reports whether the category has at least one ok signal, so its
// Normalized is meaningful.
func (c Category) determinable() bool {
	for _, e := range c.Signals {
		if e.Status == StatusOK {
			return true
		}
	}
	return false
}

// fullyNoData reports an applicable category that could not be scored at all —
// excluded from the total (weight redistributed) but flagged, unlike N/A.
func (c Category) fullyNoData() bool {
	return c.Applicable && !c.determinable() && c.hasNoData()
}

// ScoredAbsent reports a category that was actually determined and came up empty
// (normalized 0) — a real miss, not no-data. The gate's essentials floor keys on
// this so it never fails a repo merely because a signal couldn't be checked.
func (c Category) ScoredAbsent() bool {
	return c.Applicable && c.determinable() && c.Normalized == 0
}

// Scorecard is the full emitted document for one repo at one ref.
type Scorecard struct {
	Repo          string     `json:"repo"`
	Ref           string     `json:"ref"`
	ScoredAt      string     `json:"scoredAt"`
	RubricVersion string     `json:"rubricVersion"`
	Scorer        string     `json:"scorer"`
	Score         float64    `json:"score"` // 0–100, after redistribution
	Max           float64    `json:"max"`   // always 100 (the scale)
	Band          string     `json:"band"`
	DataComplete  bool       `json:"dataComplete"`
	DetectedStack []string   `json:"detectedStack,omitempty"`
	Categories    []Category `json:"categories"`
}

// Aggregate computes the /100 score from a set of scored categories, mutating
// each category's derived fields (DataComplete, BlockedBy, Contribution) in
// place, and returns the score plus whether every counted category was fully
// determined.
//
// Math: score = 100 × Σ(weightᵢ·normalizedᵢ) / Σ(weightᵢ) over the counted set —
// applicable categories that are not fully-no-data. Not-applicable and
// fully-no-data categories leave the denominator (their weight redistributes);
// fully-no-data additionally marks the score as built on partial information.
func Aggregate(cats []Category) (score float64, dataComplete bool) {
	dataComplete = true
	var denom, weightedRaw float64
	counted := make([]bool, len(cats))

	for i := range cats {
		c := &cats[i]
		if c.Normalized < 0 {
			c.Normalized = 0
		}
		if c.Normalized > 1 {
			c.Normalized = 1
		}

		// Derive DataComplete + BlockedBy from the signals.
		if c.hasNoData() {
			c.DataComplete = false
			if len(c.BlockedBy) == 0 {
				for _, e := range c.Signals {
					if e.Status == StatusNoData {
						c.BlockedBy = append(c.BlockedBy, e.Signal+": "+e.Reason)
					}
				}
			}
		} else {
			c.DataComplete = true
		}

		c.Contribution = 0
		if !c.Applicable {
			continue // not-applicable: excluded, weight redistributed, no penalty
		}
		if c.fullyNoData() {
			dataComplete = false // excluded but flagged: score is on partial info
			continue
		}
		counted[i] = true
		denom += c.Weight
		weightedRaw += c.Weight * c.Normalized
		if !c.DataComplete {
			dataComplete = false
		}
	}

	if denom == 0 {
		return 0, false
	}
	for i := range cats {
		if counted[i] {
			cats[i].Contribution = 100 * cats[i].Weight * cats[i].Normalized / denom
		}
	}
	return 100 * weightedRaw / denom, dataComplete
}

// Band classifies a /100 score. Thresholds are proportional to the v0.1 bands
// (≤6 / 7–11 / 12–14 of 14), so the calibration carries over: a 50 stays
// functional and is the default adoption baseline.
func Band(score float64) string {
	switch {
	case score < 50:
		return "needs-work"
	case score < 85:
		return "functional"
	default:
		return "exemplary"
	}
}

// Boolp is a helper for the optional Found pointer.
func Boolp(b bool) *bool { return &b }
