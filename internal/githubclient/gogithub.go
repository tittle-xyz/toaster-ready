// SPDX-License-Identifier: Apache-2.0

package githubclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
)

// GoGitHub is the Phase-B backend: a real go-github client. Every lookup that
// fails for any reason (no token, permission, network, missing data) returns a
// no-data Result with a reason — it never fails the scoring run.
type GoGitHub struct {
	gh *github.Client
}

// New builds a GoGitHub client, resolving a token from GITHUB_TOKEN and falling
// back to `gh auth token`. Returns (client, sourceDescription). If no token is
// available the client still works for public repos (unauthenticated, lower
// rate limit); auth'd calls that need a token will surface as no-data.
func New() (*GoGitHub, string) {
	token, src := resolveToken()
	c := github.NewClient(nil)
	if token != "" {
		c = c.WithAuthToken(token)
	}
	return &GoGitHub{gh: c}, src
}

// LatestRunGreen reports whether the most recent Actions run on the default
// branch concluded successfully.
func (g *GoGitHub) LatestRunGreen(slug string) Result {
	owner, name, ok := splitSlug(slug)
	if !ok {
		return NoData("unknown repo slug: " + slug)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repo, _, err := g.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return apiNoData("repo lookup", err)
	}
	branch := repo.GetDefaultBranch()

	runs, _, err := g.gh.Actions.ListRepositoryWorkflowRuns(ctx, owner, name, &github.ListWorkflowRunsOptions{
		Branch:      branch,
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return apiNoData("workflow runs", err)
	}
	if runs.GetTotalCount() == 0 || len(runs.WorkflowRuns) == 0 {
		return NoData("no workflow runs on default branch " + branch)
	}
	run := runs.WorkflowRuns[0]
	conclusion := run.GetConclusion() // success | failure | cancelled | "" (in progress)
	if run.GetStatus() != "completed" {
		return NoData(fmt.Sprintf("latest run still %s", run.GetStatus()))
	}
	return Result{
		OK:     conclusion == "success",
		Detail: fmt.Sprintf("branch=%s conclusion=%s", branch, conclusion),
	}
}

// BranchProtected reports whether the default branch has protection rules. A
// 404 means "no protection" (a real determination); a 403 means we lack the
// admin permission to read it (no-data).
func (g *GoGitHub) BranchProtected(slug string) Result {
	owner, name, ok := splitSlug(slug)
	if !ok {
		return NoData("unknown repo slug: " + slug)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repo, _, err := g.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return apiNoData("repo lookup", err)
	}
	branch := repo.GetDefaultBranch()

	prot, _, err := g.gh.Repositories.GetBranchProtection(ctx, owner, name, branch)
	if err != nil {
		// go-github returns a sentinel (not an *ErrorResponse) when the branch
		// simply isn't protected — that's a real determination, not no-data.
		if errors.Is(err, github.ErrBranchNotProtected) {
			return Result{OK: false, Detail: "no protection on " + branch}
		}
		var er *github.ErrorResponse
		if errors.As(err, &er) && er.Response != nil {
			switch er.Response.StatusCode {
			case 404:
				return Result{OK: false, Detail: "no protection on " + branch}
			case 403:
				return NoData(forbiddenReason(repo.GetPermissions(), er))
			}
		}
		return apiNoData("branch protection", err)
	}
	detail := "protected"
	if prot.GetRequiredStatusChecks() != nil {
		detail = "protected; required status checks"
	}
	return Result{OK: true, Detail: detail + " on " + branch}
}

// --- helpers ---------------------------------------------------------------

func resolveToken() (token, source string) {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, "env:GITHUB_TOKEN"
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t, "gh auth token"
		}
	}
	return "", "none (unauthenticated)"
}

func splitSlug(slug string) (owner, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(slug), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// forbiddenReason classifies a 403 into an actionable cause using GitHub's
// response message (authoritative) backed by the caller's repo permissions.
// Reading branch protection has two permission gates — the repo Admin role AND
// a token scope — but is ALSO plan-gated: it's unavailable on private repos for
// Free plans regardless of role or token. We name which lever actually applies.
func forbiddenReason(perms map[string]bool, er *github.ErrorResponse) string {
	msg := ""
	if er != nil {
		msg = er.Message
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "upgrade") || strings.Contains(lower, "make this repository public"):
		return "plan limitation (403): branch protection needs a paid plan (org Team) for private repos — not a token scope or repo role. GitHub: " + msg
	case strings.Contains(lower, "admin rights") || strings.Contains(lower, "must have admin"):
		return "repo role (403): branch protection requires the Admin role — ask an org/repo owner to grant it (a role on the repo, not a token scope)"
	case strings.Contains(lower, "not accessible by personal access token"), strings.Contains(lower, "not accessible by integration"):
		return "token scope (403): the token can't reach this endpoint — needs a classic PAT with 'repo' or a fine-grained PAT with 'Administration: read'"
	case perms != nil && !perms["admin"]:
		return "likely repo role (403): this account has '" + highestRole(perms) + "' access, but branch protection needs Admin — not a token scope"
	case msg != "":
		return "403: " + msg
	default:
		return "forbidden (403); cause unspecified by GitHub"
	}
}

// highestRole names the strongest role in a repo permissions map.
func highestRole(perms map[string]bool) string {
	switch {
	case perms["admin"]:
		return "admin"
	case perms["maintain"]:
		return "maintain"
	case perms["push"]:
		return "write"
	case perms["triage"]:
		return "triage"
	case perms["pull"]:
		return "read"
	default:
		return "none"
	}
}

func apiNoData(what string, err error) Result {
	var er *github.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		return NoData(fmt.Sprintf("%s: HTTP %d", what, er.Response.StatusCode))
	}
	return NoData(fmt.Sprintf("%s: %v", what, err))
}
