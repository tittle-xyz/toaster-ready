// SPDX-License-Identifier: Apache-2.0

package githubclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v66/github"
)

// newTestClient wires a GoGitHub at an httptest server. GoGitHub holds a
// *github.Client, so pointing its BaseURL at the fake is the whole seam — no
// interface gymnastics needed to test the real backend.
func newTestClient(t *testing.T, h http.Handler) *GoGitHub {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := github.NewClient(nil)
	u, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	c.BaseURL = u
	return &GoGitHub{gh: c}
}

// run builds the slice of a workflow-runs response.
func run(path, status, conclusion string) map[string]any {
	return map[string]any{
		"path":       path,
		"name":       strings.TrimSuffix(path, ".yml"),
		"status":     status,
		"conclusion": conclusion,
	}
}

// fakeGitHub serves /repos/o/n and its workflow runs, recording the query it was
// asked for so tests can assert on the request, not just the answer.
func fakeGitHub(t *testing.T, defaultBranch string, runs []map[string]any, gotQuery *url.Values) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/n", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": defaultBranch})
	})
	mux.HandleFunc("/repos/o/n/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if gotQuery != nil {
			q := r.URL.Query()
			*gotQuery = q
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   len(runs),
			"workflow_runs": runs,
		})
	})
	return mux
}

func TestLatestRunGreen_asksOnlyForCompletedRuns(t *testing.T) {
	// The bug this function shipped with: it asked for the newest run of any
	// status, so an in-progress run turned the whole category into no-data. That
	// made the score depend on whether anything happened to be running — including
	// the very workflow doing the scoring, when toaster runs inside Actions.
	var q url.Values
	g := newTestClient(t, fakeGitHub(t, "main", []map[string]any{
		run(".github/workflows/ci.yml", "completed", "success"),
	}, &q))

	if r := g.LatestRunGreen("o/n"); !r.OK {
		t.Fatalf("expected green, got %+v", r)
	}
	if got := q.Get("status"); got != "completed" {
		t.Errorf("status filter = %q, want %q — an in-progress run must not be able to answer this question", got, "completed")
	}
	if got := q.Get("branch"); got != "main" {
		t.Errorf("branch filter = %q, want main", got)
	}
}

func TestLatestRunGreen_inProgressRunsCannotMakeItNoData(t *testing.T) {
	// The API is asked for completed runs only, so a live run simply isn't in the
	// response. Green stays green while a release workflow is mid-flight.
	g := newTestClient(t, fakeGitHub(t, "main", []map[string]any{
		run(".github/workflows/ci.yml", "completed", "success"),
	}, nil))

	r := g.LatestRunGreen("o/n")
	if r.NoData {
		t.Fatalf("should not be no-data: %+v", r)
	}
	if !r.OK {
		t.Errorf("expected green, got %+v", r)
	}
}

func TestLatestRunGreen_aGreenReleaseCannotMaskRedTests(t *testing.T) {
	// The trap in the obvious fix. Filtering to completed runs makes the answer
	// deterministic, but "newest completed" is release-please, which succeeds
	// whether or not the tests did. Answering from it would report a green repo
	// with failing tests — worse than the flake it replaced.
	g := newTestClient(t, fakeGitHub(t, "main", []map[string]any{
		run(".github/workflows/release-please.yml", "completed", "success"), // newest
		run(".github/workflows/ci.yml", "completed", "failure"),
	}, nil))

	r := g.LatestRunGreen("o/n")
	if r.OK {
		t.Fatalf("tests failed; must not report green: %+v", r)
	}
	if !strings.Contains(r.Detail, "ci.yml") {
		t.Errorf("detail should name the workflow it judged, got %q", r.Detail)
	}
}

func TestLatestRunGreen_ignoresNonCIWorkflows(t *testing.T) {
	g := newTestClient(t, fakeGitHub(t, "main", []map[string]any{
		run(".github/workflows/docker-publish.yml", "completed", "failure"),
		run(".github/workflows/release-please.yml", "completed", "failure"),
		run(".github/workflows/ci.yml", "completed", "success"),
	}, nil))

	r := g.LatestRunGreen("o/n")
	if !r.OK {
		t.Fatalf("a failing publish workflow is not a claim about tests: %+v", r)
	}
	if !strings.Contains(r.Detail, "ci.yml") {
		t.Errorf("detail = %q, want it to name ci.yml", r.Detail)
	}
}

func TestLooksLikeCI(t *testing.T) {
	cases := map[string]bool{
		".github/workflows/ci.yml":             true,
		".github/workflows/ci.yaml":            true,
		".github/workflows/test.yml":           true,
		".github/workflows/build-test.yml":     true,
		".github/workflows/validate.yml":       true,
		".github/workflows/lint.yml":           true,
		".github/workflows/unit_tests.yml":     true,
		".github/workflows/release-please.yml": false,
		".github/workflows/docker-publish.yml": false,
		".github/workflows/deploy-staging.yml": false,
		".github/workflows/rollback.yml":       false,
		".github/workflows/security-scan.yml":  false,
		".github/workflows/codeql.yml":         false,
	}
	for p, want := range cases {
		if got := looksLikeCI(&github.WorkflowRun{Path: github.String(p)}); got != want {
			t.Errorf("looksLikeCI(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestLatestRunGreen_fallsBackWhenNothingLooksLikeCI(t *testing.T) {
	// A repo whose only automation is a deploy still gets an answer — but the
	// detail has to admit it isn't a statement about tests.
	g := newTestClient(t, fakeGitHub(t, "main", []map[string]any{
		run(".github/workflows/deploy.yml", "completed", "success"),
	}, nil))

	r := g.LatestRunGreen("o/n")
	if !r.OK {
		t.Fatalf("expected the fallback to answer green, got %+v", r)
	}
	if !strings.Contains(r.Detail, "no CI-looking workflow") {
		t.Errorf("fallback must say so; detail = %q", r.Detail)
	}
}

func TestLatestRunGreen_noCompletedRunsIsNoData(t *testing.T) {
	g := newTestClient(t, fakeGitHub(t, "main", nil, nil))

	r := g.LatestRunGreen("o/n")
	if !r.NoData {
		t.Fatalf("expected no-data, got %+v", r)
	}
	if !strings.Contains(r.Reason, "no completed workflow runs") {
		t.Errorf("reason = %q", r.Reason)
	}
}

func TestLatestRunGreen_reportsTheBranchItJudged(t *testing.T) {
	g := newTestClient(t, fakeGitHub(t, "trunk", []map[string]any{
		run(".github/workflows/ci.yml", "completed", "success"),
	}, nil))

	r := g.LatestRunGreen("o/n")
	if !strings.Contains(r.Detail, "branch=trunk") {
		t.Errorf("detail should cite the default branch it used, got %q", r.Detail)
	}
}

func TestLatestRunGreen_badSlug(t *testing.T) {
	g := newTestClient(t, fakeGitHub(t, "main", nil, nil))
	if r := g.LatestRunGreen("nope"); !r.NoData {
		t.Errorf("expected no-data for a malformed slug, got %+v", r)
	}
}

func TestLatestRunGreen_apiErrorIsNoDataNotAFailure(t *testing.T) {
	// The package contract: a lookup that can't be made is no-data, never a 0 and
	// never a crashed run.
	g := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	r := g.LatestRunGreen("o/n")
	if !r.NoData {
		t.Fatalf("expected no-data on API error, got %+v", r)
	}
	if !strings.Contains(r.Reason, "404") {
		t.Errorf("reason should carry the status code, got %q", r.Reason)
	}
}
