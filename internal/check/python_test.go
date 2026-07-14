// SPDX-License-Identifier: Apache-2.0

package check

import (
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

// The run allow-list was web-server-centric: it knew flask/uvicorn but not the
// way most Python code is actually started, silently capping every Python CLI
// at structural-only marks (issue #22).
func TestRunCommandRecognizesPythonEntryPoints(t *testing.T) {
	cases := []struct {
		name string
		code string
		want bool
	}{
		{"python -m package", "python -m wine_geo --provider mock --seed 42\n", true},
		{"python3 -m package", "python3 -m wine_geo\n", true},
		{"python -m dotted module", "python -m http.server 8000\n", true},
		{"python script", "python scripts/serve.py\n", true},
		{"python3 script with path", "python3 ./cmd/app.py --debug\n", true},
		{"uv run target", "uv run wine-geo --n 30\n", true},
		{"uv run python -m", "uv run python -m wine_geo\n", true},
		{"poetry run target", "poetry run wine-geo\n", true},
		{"pipenv run python -m", "pipenv run python -m app\n", true},
		{"dagster dev", "dagster dev\n", true},
		{"streamlit run", "streamlit run app.py\n", true},
		{"fastapi dev", "fastapi dev main.py\n", true},
		{"celery worker", "celery -A proj worker --loglevel=info\n", true},

		// The allow-list is about *starting* something. These build, test, or
		// lint — the same line `make build` and `npm test` sit on.
		{"python -m pytest is a test", "python -m pytest tests/\n", false},
		{"python -m pytest with a .py arg", "python -m pytest tests/test_app.py\n", false},
		{"python -m build is a build", "python -m build\n", false},
		{"python -m venv is setup", "python -m venv .venv\n", false},
		{"python -m pip is setup", "python -m pip install -r requirements.txt\n", false},
		{"uv run pytest is a test", "uv run pytest\n", false},
		{"poetry run ruff is lint", "poetry run ruff check .\n", false},
		{"uv run python -m pytest is a test", "uv run python -m pytest\n", false},
		{"setup.py packages, it doesn't start", "python setup.py sdist bdist_wheel\n", false},
		{"setup.py install is not a run", "python setup.py install\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRunCommand(tc.code); got != tc.want {
				t.Errorf("hasRunCommand(%q) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}

// End-to-end: a Python CLI documenting `python -m pkg` in a fenced block reaches
// full marks, where before it was capped at structure-only.
func TestPythonCLIRunbookScoresFullSetup(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"x\"\n",
		"Makefile":       "test:\n\techo test\n",
		"README.md":      "## Usage\n\n```sh\npython -m wine_geo --provider mock\n```\n",
	}))
	if got := cat(sc, scorecard.CatSetup).Normalized; got != 1 {
		t.Errorf("python CLI setup = %v, want 1.0", got)
	}
}

// PEP 518 consolidates tool config in pyproject.toml; matching only standalone
// config files made the convention invisible (issue #22).
func TestPyprojectToolTablesCountAsLintConfig(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"x\"\n\n[tool.ruff]\nline-length = 100\n\n[tool.ruff.lint]\nselect = [\"E\"]\n\n[tool.mypy]\nstrict = true\n",
	}))
	if !found(t, cat(sc, scorecard.CatConventions), "lint/format configs") {
		t.Error("[tool.ruff] in pyproject.toml should count as a lint config")
	}
}

// Packaging config is not a lint standard — only the named lint tools count.
func TestPyprojectPackagingIsNotLintConfig(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"x\"\n\n[tool.poetry]\nname = \"x\"\n\n[tool.setuptools]\nzip-safe = false\n",
	}))
	if found(t, cat(sc, scorecard.CatConventions), "lint/format configs") {
		t.Error("[tool.poetry] is packaging config, not a lint standard")
	}
}

// [tool.coverage] is coverage.py's canonical home — the same PEP 518 gap.
func TestPyprojectCoverageTableCountsAsCoverage(t *testing.T) {
	sc := scoreDir(t, writeRepo(t, map[string]string{
		"pyproject.toml":      "[project]\nname = \"x\"\n\n[tool.coverage.run]\nbranch = true\n",
		"tests/test_app.py":   "def test_x():\n    assert True\n",
		"src/app/__init__.py": "x = 1\n",
	}))
	if !found(t, cat(sc, scorecard.CatTesting), "coverage reporting") {
		t.Error("[tool.coverage] in pyproject.toml should count as coverage reporting")
	}
}
