// SPDX-License-Identifier: Apache-2.0

package check

import "testing"

// The secret floor is deliberately basic — thorough scanning is gitleaks' job in
// the pipeline. "Basic" still has to mean *correct*, though, so these lock in both
// halves: the shapes that must trip it, and the shapes that must not.
//
// Both of the first two cases are regressions. The original rules matched only an
// unquoted key, so a secret in a map or JSON literal was invisible; and they keyed
// off whole words, so `dbPass` was invisible. Neither was caught by any test.
func TestSecretRulesDetect(t *testing.T) {
	tests := []struct {
		name string
		line string
		rule string
	}{
		{"quoted key in a map literal", `var m = map[string]string{"password": "s3cr3tvalue99x"}`, "assigned-credential"},
		{"credential-ish variable name", `const dbPass = "hX7qP2mZ9vLk"`, "credential-ish-name"},
		{"unquoted key", `password = "hX7qP2mZ9vLkQ"`, "assigned-credential"},
		{"json body", `{"client_secret": "aB3dE5gH7jK9mN1p"}`, "assigned-credential"},
		{"aws access key id", `const k = "AKIA2X7QFJ3LMNBVCZQP"`, "aws-access-key-id"},
		{"github token", `token := "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"`, "github-token"},
		{"slack token", `t := "xoxb-2f3K9dLm1QpZ7rTvXy"`, "slack-token"},
		{"google api key", `k := "AIzaSyA1b2C3d4E5f6G7h8I9j0K1l2M3n4O5pQr"`, "google-api-key"},
		{"private key block", `-----BEGIN RSA PRIVATE KEY-----`, "private-key-block"},
		// Regression: an earlier slug guard allowed digits in segments, which made
		// this lowercase-with-underscores key look like an identifier and stopped
		// detecting it. Caught by TestUntrackedUnignoredEnvStillTripsSecretFloor.
		{"stripe-shaped key in an assignment", `api_key = "sk_live_abcd1234efgh5678"`, "assigned-credential"},
		{"jwt literal", `t := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"`, "jwt-literal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, ok := firstMatchingRule(tt.line)
			if !ok {
				t.Fatalf("no rule matched %q", tt.line)
			}
			if rule != tt.rule {
				t.Errorf("matched %q, want %q", rule, tt.rule)
			}
		})
	}
}

func TestSecretRulesIgnore(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		// secretIgnore: documentation and templates must never fail a build.
		{"placeholder value", `password = "your_password_here"`},
		{"changeme", `secret = "changeme_please_now"`},
		{"angle-bracket placeholder", `api_key = "<YOUR_API_KEY_HERE>"`},
		{"shell interpolation", `password = "${DB_PASSWORD}"`},
		// The canonical AWS example key contains "EXAMPLE" and is correctly
		// ignored. Worth pinning: testing with it once produced a false pass and
		// nearly led to reporting the floor as broken.
		{"canonical aws example key", `const k = "AKIAIOSFODNN7EXAMPLE"`},

		// Entropy gate: these match a rule's shape but are plainly not secrets.
		{"low-entropy repeated value", `password = "aaaaaaaaaaaa"`},
		{"a filesystem path", `secretPath = "/etc/app/secret/file"`},
		{"an English sentence", `passMsg = "the tests all passed today"`},
		{"a short word", `token = "shortish"`},

		// Slug-shaped constants. Entropy cannot separate these: "config-and-secrets"
		// scores 3.61, *higher* than the real credential "hX7qP2mZ9vLk" at 3.58. So
		// the guard is on shape, not on a higher threshold — raising the bar would
		// have discarded real secrets instead.
		{"kebab-case category constant", `CatConfigSecrets = "config-and-secrets"`},
		{"another kebab constant", `CatTesting = "testing-and-coverage"`},
		{"snake_case constant", `tokenKind = "refresh_token_grant"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rule, ok := firstMatchingRule(tt.line); ok {
				t.Errorf("rule %q tripped on %q, expected no hit", rule, tt.line)
			}
		})
	}
}

func TestShannonEntropy(t *testing.T) {
	// Ordering is what matters, not exact values: a random-looking credential must
	// out-score a repeated character and an English word.
	repeated := shannonEntropy("aaaaaaaaaaaa")
	word := shannonEntropy("password")
	random := shannonEntropy("hX7qP2mZ9vLk")

	if !(repeated < word && word < random) {
		t.Errorf("expected repeated(%.2f) < word(%.2f) < random(%.2f)", repeated, word, random)
	}
	if got := shannonEntropy(""); got != 0 {
		t.Errorf("shannonEntropy(\"\") = %v, want 0", got)
	}
}

// firstMatchingRule delegates to production so these tests cannot drift from the
// real decision — an earlier local copy did exactly that.
func firstMatchingRule(line string) (string, bool) { return matchSecretRule(line) }
