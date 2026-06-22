// SPDX-License-Identifier: Apache-2.0

package scorecard

// Rubric-v2 category IDs. Stable identifiers used by checkers, config, and
// the gate.
const (
	CatAgentInstructions  = "agent-instructions"
	CatSetup              = "setup-reproducibility"
	CatTesting            = "testing-and-coverage"
	CatCICD               = "cicd-pipeline"
	CatConfigSecrets      = "config-and-secrets"
	CatPurpose            = "purpose-and-orientation"
	CatConventions        = "conventions-and-standards"
	CatSourceTrail        = "source-material-trail"
	CatInRepoTooling      = "in-repo-tooling"
	CatDependencyPatching = "dependency-patching"
	CatDBMigrations       = "db-migrations"
)

// CategoryDef is a category's identity and default weight in the rubric.
type CategoryDef struct {
	ID          string
	Title       string
	Weight      float64
	Conditional bool // may be not-applicable for some repos (e.g. db-migrations)
}

// DefaultRubricV2 is the maintainer's opinionated default catalog (ADR-0002,
// decision 2). Weights sum to 100; config overrides them. The category set
// is fixed — config tunes weights/thresholds/signals, not the list.
var DefaultRubricV2 = []CategoryDef{
	{CatAgentInstructions, "Agent/human instructions", 15, false},
	{CatSetup, "Setup reproducibility", 12, false},
	{CatTesting, "Testing & coverage", 12, false},
	{CatCICD, "CI: test / build / deploy", 12, false},
	{CatConfigSecrets, "Config & secrets", 10, false},
	{CatPurpose, "Purpose & orientation", 10, false},
	{CatConventions, "Conventions & standards", 8, false},
	{CatSourceTrail, "Source-material trail", 7, false},
	{CatInRepoTooling, "In-repo tooling", 6, false},
	{CatDependencyPatching, "Dependency patching", 5, false},
	{CatDBMigrations, "DB migrations", 3, true},
}

var rubricByID = func() map[string]CategoryDef {
	m := make(map[string]CategoryDef, len(DefaultRubricV2))
	for _, d := range DefaultRubricV2 {
		m[d.ID] = d
	}
	return m
}()

// Def returns the default definition for a category id (zero value if unknown).
func Def(id string) CategoryDef { return rubricByID[id] }

// KnownCategory reports whether id is a category in the rubric.
func KnownCategory(id string) bool { _, ok := rubricByID[id]; return ok }

// WeightFor returns the default weight for a category id.
func WeightFor(id string) float64 { return rubricByID[id].Weight }

// TitleFor returns the default title for a category id.
func TitleFor(id string) string { return rubricByID[id].Title }

// DefaultWeightSum totals the default weights (100 for a valid catalog).
func DefaultWeightSum() float64 {
	var sum float64
	for _, d := range DefaultRubricV2 {
		sum += d.Weight
	}
	return sum
}
