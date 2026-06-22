// SPDX-License-Identifier: Apache-2.0

package check

import (
	"fmt"
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
