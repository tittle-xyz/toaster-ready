// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// GitHub Actions evaluates expression syntax ANYWHERE in action.yml, including
// input descriptions — and the `github` context is not in scope there. A single
// one written into a description fails the whole action at load time, for every
// consumer, with "Unrecognized named-value: 'github'".
//
// v0.7.0 shipped exactly that: a `${{ github.head_ref }}` inside prose explaining
// how to use the `commit` input. The manifest is valid YAML, `go build` is happy,
// and every test passed — it only fails on a runner, at which point it is already
// released and every consumer pinned to the major tag is broken.
//
// So: no expression syntax in input descriptions. Defaults are a different matter
// (`token` legitimately defaults to a github context value), and the `runs:`
// section is where expressions belong.
func TestActionInputDescriptionsHaveNoExpressions(t *testing.T) {
	raw, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatalf("read action.yml: %v", err)
	}

	var manifest struct {
		Inputs map[string]struct {
			Description string `yaml:"description"`
		} `yaml:"inputs"`
	}
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}
	if len(manifest.Inputs) == 0 {
		t.Fatal("no inputs parsed from action.yml — the assertion below would pass vacuously")
	}

	for name, in := range manifest.Inputs {
		if strings.Contains(in.Description, "${{") {
			t.Errorf("input %q has expression syntax in its description; "+
				"Actions evaluates it and fails the action at load time. "+
				"Name the context value instead of writing an expression.", name)
		}
	}
}
