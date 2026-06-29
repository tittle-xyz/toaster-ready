// SPDX-License-Identifier: Apache-2.0

package check

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tittle-xyz/toaster-ready/internal/repo"
	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

// --- evidence constructors -------------------------------------------------

func evFound(signal, method, path string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusOK, Found: scorecard.Boolp(true), Path: path, Source: "filesystem"}
}

func evAbsent(signal, method, path string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusOK, Found: scorecard.Boolp(false), Path: path, Source: "filesystem"}
}

func evNote(signal, method, path, note string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusOK, Found: scorecard.Boolp(true), Path: path, Source: "filesystem", Note: note}
}

// sourceForMethod keeps a signal's provenance honest — derived from how it was
// gathered rather than hardcoded.
func sourceForMethod(method string) string {
	switch method {
	case scorecard.MethodAPI:
		return "github-api"
	case scorecard.MethodSkill:
		return "skill+mcp"
	default:
		return "filesystem"
	}
}

func evNoData(signal, method, reason string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusNoData, Reason: reason, Source: sourceForMethod(method)}
}

func evFoundDetail(signal, method, detail string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusOK, Found: scorecard.Boolp(true), Source: "github-api", Note: detail}
}

func evNotFoundDetail(signal, method, detail string) scorecard.Evidence {
	return scorecard.Evidence{Signal: signal, Method: method, Status: scorecard.StatusOK, Found: scorecard.Boolp(false), Source: "github-api", Note: detail}
}

// --- text helpers ----------------------------------------------------------

func countHeadings(md string) int {
	n := 0
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			n++
		}
	}
	return n
}

// linkRe counts only real source-material links — a URL to Atlassian/Confluence
// or a Jira /browse/KEY-N path. It deliberately does NOT match the bare words
// "confluence"/"atlassian" or a bare KEY-N in prose: those produce false
// positives (e.g. a README that merely mentions Confluence), and the actual
// resolution of links is the skill+MCP layer's job anyway.
var linkRe = regexp.MustCompile(`https?://[^\s)"'<>]*(?:atlassian\.net|confluence)[^\s)"'<>]*|/browse/[A-Z][A-Z0-9]+-\d+`)

func countLinks(md string) int {
	return len(linkRe.FindAllString(md, -1))
}

// hasSetupSection reports whether a README has a section that documents how to
// get the project running — the "single documented path" signal.
var setupSectionRe = regexp.MustCompile(`(?im)^#{1,6}\s*(get(ting)?\s+started|setup|set\s+up|install(ation|ing)?|build(ing)?|usage|quick\s*start|development|running)\b`)

func hasSetupSection(md string) bool {
	return setupSectionRe.MatchString(md)
}

// envVarRes extract the NAME of an environment variable read by code, across
// the common languages. Used to tell "needs a config sheet" from "needs no
// config" in the config-and-secrets dimension.
var envVarRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)getenv\(\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]{2,})`),
	regexp.MustCompile(`process\.env\[\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	regexp.MustCompile(`\$_(?:ENV|SERVER)\[\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	regexp.MustCompile(`os\.environ(?:\.get)?[(\[]\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	regexp.MustCompile(`\bENV\[\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	regexp.MustCompile(`System\.getenv\(\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
}

// envNoise are ambient OS/CI vars that don't represent app configuration.
var envNoise = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "PWD": true, "SHELL": true,
	"TERM": true, "LANG": true, "TMPDIR": true, "GOPATH": true, "GOROOT": true,
	"CI": true, "DEBUG": true, "EDITOR": true, "HOSTNAME": true,
}

func referencedEnvVars(r *repo.Repo) []string {
	set := map[string]bool{}
	for _, rel := range r.Files() {
		if skipForSecrets(rel) {
			continue
		}
		body, err := r.Read(rel)
		if err != nil {
			continue
		}
		for _, re := range envVarRes {
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				if name := m[1]; !envNoise[name] {
					set[name] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func boolN(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// --- instruction command-drift detection -----------------------------------

// codeText returns only the code in a markdown document — the contents of
// fenced ``` blocks plus inline `code` spans — joined by newlines. Command-drift
// detection runs over this, not the prose, so phrases like "make sure" or "just
// works" can't masquerade as a `make`/`just` invocation.
var (
	fencedRe = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")
	inlineRe = regexp.MustCompile("`([^`\n]+)`")
)

func codeText(md string) string {
	var b strings.Builder
	for _, m := range fencedRe.FindAllStringSubmatch(md, -1) {
		b.WriteString(m[1])
		b.WriteByte('\n')
	}
	// Strip fenced blocks before scanning inline spans so their backticks don't
	// double-count, then collect inline `code`.
	rest := fencedRe.ReplaceAllString(md, "\n")
	for _, m := range inlineRe.FindAllStringSubmatch(rest, -1) {
		b.WriteString(m[1])
		b.WriteByte('\n')
	}
	return b.String()
}

// command-reference patterns, scoped to code only (see codeText). Each captures
// the target/script/recipe name in group 1.
var (
	makeCmdRe = regexp.MustCompile(`\bmake\s+([A-Za-z0-9_][A-Za-z0-9_.-]*)`)
	npmCmdRe  = regexp.MustCompile(`\b(?:npm|pnpm|yarn|bun)\s+run\s+([A-Za-z0-9_][A-Za-z0-9_:.-]*)`)
	justCmdRe = regexp.MustCompile(`\bjust\s+([A-Za-z0-9_][A-Za-z0-9_.-]*)`)
)

// makeTargetRe matches a Makefile target definition (start of line, before ':').
var makeTargetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*:(?:[^=]|$)`)

// commandDrift returns the command invocations referenced in the instructions'
// code that do NOT resolve to a real target in the repo's manifests: a `make X`
// with no `X` target (or no Makefile), an `npm/pnpm/yarn/bun run X` absent from
// package.json scripts (or no package.json), or a `just X` absent from the
// justfile. Each hit is a short human-readable label. A referenced runner whose
// manifest is missing entirely is a hit too — the documented command can't run.
// Conservative by design: it only flags concrete, named targets found in code,
// never bare tool names or dependencies mentioned in prose.
func commandDrift(r *repo.Repo, instrBody string) []string {
	code := codeText(instrBody)
	var hits []string
	seen := map[string]bool{}
	add := func(label string) {
		if !seen[label] {
			seen[label] = true
			hits = append(hits, label)
		}
	}

	// make
	if refs := captureAll(makeCmdRe, code); len(refs) > 0 {
		targets := makeTargets(r)
		for _, t := range refs {
			if !targets[t] {
				add("make " + t)
			}
		}
	}
	// npm/pnpm/yarn/bun run
	if refs := captureAll(npmCmdRe, code); len(refs) > 0 {
		scripts := packageScripts(r)
		for _, s := range refs {
			if !scripts[s] {
				add("npm run " + s)
			}
		}
	}
	// just
	if refs := captureAll(justCmdRe, code); len(refs) > 0 {
		recipes := justRecipes(r)
		for _, t := range refs {
			if !recipes[t] {
				add("just " + t)
			}
		}
	}
	sort.Strings(hits)
	return hits
}

func captureAll(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// makeTargets returns the set of targets declared in the repo's Makefile (empty
// when there is none, which makes any documented `make X` a drift hit).
func makeTargets(r *repo.Repo) map[string]bool {
	set := map[string]bool{}
	mk := r.FirstExisting("Makefile", "makefile", "GNUmakefile")
	if mk == "" {
		return set
	}
	body, err := r.Read(mk)
	if err != nil {
		return set
	}
	// makeTargetRe's leading [A-Za-z0-9_] already excludes directives like .PHONY.
	for _, m := range makeTargetRe.FindAllStringSubmatch(body, -1) {
		set[m[1]] = true
	}
	return set
}

// packageScripts returns the set of npm script names from package.json.
func packageScripts(r *repo.Repo) map[string]bool {
	set := map[string]bool{}
	body, err := r.Read("package.json")
	if err != nil {
		return set
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(body), &pkg) != nil {
		return set
	}
	for name := range pkg.Scripts {
		set[name] = true
	}
	return set
}

// justRecipes returns the set of recipe names declared in the repo's justfile.
func justRecipes(r *repo.Repo) map[string]bool {
	set := map[string]bool{}
	jf := r.FirstExisting("justfile", "Justfile", ".justfile")
	if jf == "" {
		return set
	}
	body, err := r.Read(jf)
	if err != nil {
		return set
	}
	// Recipes look like `name:` or `name arg:` at column 0; skip assignments and
	// comments. Reuse makeTargetRe's shape (recipe header before ':').
	for _, m := range makeTargetRe.FindAllStringSubmatch(body, -1) {
		set[m[1]] = true
	}
	return set
}

// --- runnable runbook detection --------------------------------------------

// runCmdRe matches a recognized "run the app" command (issue #3's allow-list).
// It is about *starting* something — a dev server, a container, a binary — not
// building or testing it, so `make build` / `npm test` deliberately do not match.
var runCmdRe = regexp.MustCompile(`(?i)\b(?:` +
	`docker(?:-|\s)compose\s+up` +
	`|docker\s+run` +
	`|(?:make|just)\s+(?:run|dev|start|serve|up|watch)\b` +
	`|(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:dev|start|serve|preview)\b` +
	`|go\s+run\b` +
	`|cargo\s+run\b` +
	`|dotnet\s+run\b` +
	`|flask\s+run\b` +
	`|(?:uvicorn|gunicorn|hypercorn)\b` +
	`|manage\.py\s+runserver` +
	`|rails\s+(?:server|s)\b` +
	`|artisan\s+serve` +
	`|gradlew\b[^\n]*\bbootRun` +
	`)`)

// endpointRe spots a reachable local endpoint/port near the run command — the
// "and here's where the app comes up" half of issue #3. It strengthens the
// evidence note but is not required (a CLI has no port).
var endpointRe = regexp.MustCompile(`(?i)(?:localhost|127\.0\.0\.1|0\.0\.0\.0):\d{2,5}|https?://localhost|\bport\s+\d{2,5}\b|:\d{3,5}\b`)

// fencedText returns only the contents of fenced ``` code blocks, joined by
// newlines. The runbook lives in a fenced block an agent can copy-paste — not an
// inline mention in prose (e.g. a rubric table that merely lists `make run` as an
// example), which would be a false positive.
func fencedText(md string) string {
	var b strings.Builder
	for _, m := range fencedRe.FindAllStringSubmatch(md, -1) {
		b.WriteString(m[1])
		b.WriteByte('\n')
	}
	return b.String()
}

// runnableRunbook reports whether the agent-instructions file or README contains
// a copy-pasteable run command — one an agent can execute without inferring it —
// inside a fenced code block, and from which file.
func runnableRunbook(r *repo.Repo) (src string, hasEndpoint bool, ok bool) {
	for _, f := range runbookSources(r) {
		body, err := r.Read(f)
		if err != nil {
			continue
		}
		fenced := fencedText(body)
		if runCmdRe.MatchString(fenced) {
			return f, endpointRe.MatchString(fenced), true
		}
	}
	return "", false, false
}

// runbookSources lists the files a runnable run command may live in, agent
// instructions first (that's where an agent looks), then the README.
func runbookSources(r *repo.Repo) []string {
	var srcs []string
	if f := r.FirstExisting("CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md"); f != "" {
		srcs = append(srcs, f)
	}
	if f := r.FirstExisting("README.md", "README.rst", "README.txt", "README"); f != "" {
		srcs = append(srcs, f)
	}
	return srcs
}

// --- local datastore provisioning (issue #4) -------------------------------

// dbImageTokens are container-image name fragments that identify a database
// service in a compose file — the "you can bring the DB up locally" signal.
var dbImageTokens = []string{
	"postgres", "postgis", "timescale", "mysql", "mariadb", "percona",
	"mongo", "redis", "valkey", "memcached", "cockroach", "clickhouse",
	"cassandra", "scylla", "neo4j", "couchdb", "couchbase", "rethinkdb",
	"mssql", "sqlserver", "elasticsearch", "opensearch", "influxdb",
}

var composeImageRe = regexp.MustCompile(`(?im)^\s*image:\s*["']?([^\s"']+)`)

// composeDBService reports whether a docker-compose / compose file declares a
// service whose image is a recognized database, and returns the matched image
// and the file it came from. This is the "local data store you can bring up"
// half of clone→running for a DB-backed app.
func composeDBService(r *repo.Repo) (image, where string, ok bool) {
	var files []string
	for _, pat := range []string{"docker-compose*.yml", "docker-compose*.yaml", "compose*.yml", "compose*.yaml"} {
		files = append(files, r.Glob(pat)...)
	}
	for _, f := range files {
		body, err := r.Read(f)
		if err != nil {
			continue
		}
		for _, m := range composeImageRe.FindAllStringSubmatch(body, -1) {
			name := imageName(m[1])
			for _, tok := range dbImageTokens {
				if strings.Contains(name, tok) {
					return m[1], f, true
				}
			}
		}
	}
	return "", "", false
}

// imageName reduces a container image reference to its lowercase short name —
// registry, org, and tag stripped (e.g. "bitnami/postgresql:15" -> "postgresql").
func imageName(ref string) string {
	ref = strings.ToLower(ref)
	if i := strings.IndexAny(ref, "@:"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	return ref
}

// hasSeed reports whether the repo ships seed data or a seed runner — the
// "populate it so it's usable" half. It recognizes seed files/dirs and a seed
// target in a Makefile / package.json / justfile.
func hasSeed(r *repo.Repo) (where string, ok bool) {
	for _, rel := range r.Files() {
		low := filepath.ToSlash(strings.ToLower(rel))
		base := strings.ToLower(filepath.Base(rel))
		seedFile := base == "seed" || base == "seeds" ||
			strings.HasPrefix(base, "seed.") || strings.HasPrefix(base, "seeds.")
		seedDir := strings.HasPrefix(low, "seed/") || strings.HasPrefix(low, "seeds/") ||
			strings.Contains(low, "/seed/") || strings.Contains(low, "/seeds/") ||
			strings.Contains(low, "db/seed")
		if seedFile || seedDir {
			return rel, true
		}
	}
	if t := seedTarget(r); t != "" {
		return t, true
	}
	return "", false
}

// seedTarget finds a seed runner declared in a task manifest, reusing the
// command-drift manifest parsers.
func seedTarget(r *repo.Repo) string {
	for name := range makeTargets(r) {
		if strings.Contains(name, "seed") {
			return "Makefile"
		}
	}
	for name := range packageScripts(r) {
		if strings.Contains(name, "seed") {
			return "package.json"
		}
	}
	for name := range justRecipes(r) {
		if strings.Contains(name, "seed") {
			return "justfile"
		}
	}
	return ""
}

// --- secret floor (regex, v0.1; gitleaks swaps in later) -------------------

type secretHit struct {
	Path string
	Ref  string // line locator
	Rule string
}

var secretRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"aws-access-key-id", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"private-key-block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`)},
	{"assigned-credential", regexp.MustCompile(`(?i)(?:password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret)\s*(?:=|:|=>)\s*['"][^'"\s]{8,}['"]`)},
	{"php-define-secret", regexp.MustCompile(`(?i)define\s*\(\s*['"][A-Z0-9_]*(?:KEY|SECRET|PASSWORD|TOKEN)[A-Z0-9_]*['"]\s*,\s*['"][^'"\s]{6,}['"]`)},
	{"jwt-literal", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
}

// placeholders we should not flag (example/template values)
var secretIgnore = regexp.MustCompile(`(?i)(your[_-]?|example|changeme|placeholder|xxxx|<.*>|\$\{)`)

func scanSecrets(r *repo.Repo) []secretHit {
	var hits []secretHit
	for _, rel := range r.Files() {
		if skipForSecrets(rel) {
			continue
		}
		body, err := r.Read(rel)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(body, "\n") {
			if len(line) > 500 {
				continue
			}
			for _, rule := range secretRules {
				if rule.re.MatchString(line) && !secretIgnore.MatchString(line) {
					hits = append(hits, secretHit{Path: rel, Ref: fmt.Sprintf("L%d", i+1), Rule: rule.name})
					break
				}
			}
		}
	}
	return hits
}

func skipForSecrets(rel string) bool {
	low := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(low, ".example"), strings.HasSuffix(low, ".sample"), strings.HasSuffix(low, ".template"):
		return true
	case strings.Contains(low, ".env.example"), strings.Contains(low, "test"), strings.Contains(low, "fixture"), strings.Contains(low, "mock"):
		return true
	case strings.HasSuffix(low, ".lock"), strings.HasSuffix(low, ".sum"), strings.HasSuffix(low, ".min.js"), strings.HasSuffix(low, ".map"):
		return true
	}
	return false
}
