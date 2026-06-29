// SPDX-License-Identifier: Apache-2.0

// Package repo acquires a target repository (local path or remote clone) and
// exposes the cheap filesystem/git facts the deterministic checkers need.
package repo

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cloneTimeout bounds shallow clones of remote slugs so a hostile or huge repo
// can't hang the process indefinitely.
const cloneTimeout = 2 * time.Minute

// maxWalkFiles caps the file walk so a repo with a pathological number of files
// can't exhaust memory/time.
const maxWalkFiles = 50000

// Repo is a checked-out repository ready to score.
type Repo struct {
	Root      string // absolute path to the working tree
	Ref       string // resolved HEAD sha, or "" if not a git repo
	Slug      string // owner/name when known (for API + reporting)
	tmpClone  bool   // true if we cloned into a temp dir and should clean up
	fileCache []string
}

// Open resolves target into a Repo. target is either a local path or an
// "owner/name" slug, in which case it is cloned via git into a temp dir.
func Open(target string) (*Repo, error) {
	if isLocalPath(target) {
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("local path %q: %w", target, err)
		}
		r := &Repo{Root: abs, Slug: slugFromRemote(abs)}
		r.Ref = gitHead(abs)
		return r, nil
	}

	// Remote slug -> shallow clone via git (exec, per v0.1 decision).
	dir, err := os.MkdirTemp("", "toaster-clone-*")
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://github.com/%s.git", target)
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", url, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("git clone %s: %v: %s", target, err, strings.TrimSpace(string(out)))
	}
	r := &Repo{Root: dir, Ref: gitHead(dir), Slug: target, tmpClone: true}
	return r, nil
}

// Close removes a temp clone, if any.
func (r *Repo) Close() error {
	if r.tmpClone {
		return os.RemoveAll(r.Root)
	}
	return nil
}

// resolve joins rel to the repo root and confirms the result stays within the
// tree. It defeats `..` traversal (e.g. from an attacker-controlled @path import
// in a scored repo's CLAUDE.md). Returns ok=false if rel would escape the root.
func (r *Repo) resolve(rel string) (string, bool) {
	p := filepath.Join(r.Root, rel)
	rp, err := filepath.Rel(r.Root, p)
	if err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// Exists reports whether a relative path exists in the working tree. Paths that
// escape the root are treated as nonexistent.
func (r *Repo) Exists(rel string) bool {
	p, ok := r.resolve(rel)
	if !ok {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// FirstExisting returns the first relative path that exists, or "".
func (r *Repo) FirstExisting(rels ...string) string {
	for _, rel := range rels {
		if r.Exists(rel) {
			return rel
		}
	}
	return ""
}

// Read returns the contents of a relative path. Paths that escape the root are
// refused.
func (r *Repo) Read(rel string) (string, error) {
	p, ok := r.resolve(rel)
	if !ok {
		return "", fmt.Errorf("path %q escapes the repository root", rel)
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// Glob matches a shell pattern relative to the root (single dir level).
func (r *Repo) Glob(pattern string) []string {
	matches, _ := filepath.Glob(filepath.Join(r.Root, pattern))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, _ := filepath.Rel(r.Root, m)
		out = append(out, rel)
	}
	return out
}

// Files lazily walks the tree once, skipping noise dirs, and caches the result
// as relative paths. Used by the secret scanner.
func (r *Repo) Files() []string {
	if r.fileCache != nil {
		return r.fileCache
	}
	skip := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, ".terraform": true,
		"dist": true, "build": true, ".next": true, "__pycache__": true,
	}
	var out []string
	_ = filepath.Walk(r.Root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 512*1024 { // skip large/binary-ish files
			return nil
		}
		if len(out) >= maxWalkFiles { // bound pathological repos
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(r.Root, p)
		out = append(out, rel)
		return nil
	})
	r.fileCache = out
	return out
}

// GitTags returns local git tags (used as a semver signal).
func (r *Repo) GitTags() []string {
	out, err := exec.Command("git", "-C", r.Root, "tag").Output()
	if err != nil {
		return nil
	}
	var tags []string
	s := bufio.NewScanner(strings.NewReader(string(out)))
	for s.Scan() {
		if t := strings.TrimSpace(s.Text()); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// IsShallow reports whether the working tree is a shallow clone — one without
// full history. The drift checker's staleness signal is meaningless on a
// shallow tree (a `--depth 1` clone has a single commit that "touched" every
// file), so it must surface no-data rather than trust the truncated log.
func (r *Repo) IsShallow() bool {
	out, err := exec.Command("git", "-C", r.Root, "rev-parse", "--is-shallow-repository").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// InstructionsChurn measures how stale instrPath is against source churn: it
// returns the number of commits touching non-doc source *after* the commit that
// last modified instrPath (since), the total number of such source commits in
// history (total), and ok. ok=false means git history is unavailable — not a
// git repo, a shallow clone, or instrPath is untracked — and the caller MUST
// treat that as no-data, never as "fresh". Markdown is excluded from "source"
// so doc-only churn doesn't read as the codebase moving out from under the docs.
func (r *Repo) InstructionsChurn(instrPath string) (since, total int, ok bool) {
	if r.Ref == "" || r.IsShallow() {
		return 0, 0, false
	}
	sha := r.lastCommitSha(instrPath)
	if sha == "" {
		return 0, 0, false // file untracked / no history for it
	}
	// Pathspec: everything except markdown (top-level and nested).
	srcSpec := []string{".", ":(exclude)*.md", ":(exclude,glob)**/*.md"}
	total, okT := r.countCommits("HEAD", srcSpec)
	since, okS := r.countCommits(sha+"..HEAD", srcSpec)
	if !okT || !okS {
		return 0, 0, false
	}
	return since, total, true
}

// lastCommitSha returns the SHA of the most recent commit that touched rel, or
// "" if rel has no history (untracked, or not a git repo).
func (r *Repo) lastCommitSha(rel string) string {
	out, err := exec.Command("git", "-C", r.Root, "log", "-1", "--format=%H", "--", rel).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// countCommits runs `git rev-list --count <revRange> -- <pathspec...>` and
// returns the count. ok=false on any git error or unparseable output.
func (r *Repo) countCommits(revRange string, pathspec []string) (int, bool) {
	args := []string{"-C", r.Root, "rev-list", "--count", revRange}
	if len(pathspec) > 0 {
		args = append(args, "--")
		args = append(args, pathspec...)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return n, true
}

func isLocalPath(target string) bool {
	if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") {
		return true
	}
	// owner/name has exactly one slash and no path-ish segments
	if _, err := os.Stat(target); err == nil {
		return true
	}
	return false
}

func gitHead(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// slugFromRemote best-effort extracts owner/name from origin's URL.
func slugFromRemote(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, "github.com"); i >= 0 {
		rest := url[i+len("github.com"):]
		rest = strings.TrimLeft(rest, ":/")
		return rest
	}
	return ""
}
