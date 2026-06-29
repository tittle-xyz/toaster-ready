// SPDX-License-Identifier: Apache-2.0

package check

import (
	"testing"

	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

func TestComposeDBServiceDetection(t *testing.T) {
	cases := []struct {
		name    string
		compose string
		want    bool
	}{
		{"postgres", "services:\n  db:\n    image: postgres:16\n", true},
		{"bitnami postgresql", "services:\n  db:\n    image: bitnami/postgresql:15\n", true},
		{"redis", "services:\n  cache:\n    image: redis:7-alpine\n", true},
		{"mongo quoted", "services:\n  m:\n    image: \"mongo:7\"\n", true},
		{"app image only", "services:\n  web:\n    image: nginx:latest\n", false},
		{"no image (build)", "services:\n  web:\n    build: .\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "docker-compose.yml", tc.compose)
			_, _, ok := composeDBService(openLocal(t, dir))
			if ok != tc.want {
				t.Errorf("composeDBService(%q) = %v, want %v", tc.compose, ok, tc.want)
			}
		})
	}
}

func TestHasSeedDetection(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"seed file", map[string]string{"db/seed.sql": "insert ..."}, true},
		{"seeds dir", map[string]string{"seeds/001.sql": "insert ..."}, true},
		{"rails seeds", map[string]string{"db/seeds.rb": "User.create"}, true},
		{"make target", map[string]string{"Makefile": "seed:\n\tgo run ./seed\n"}, true},
		{"npm script", map[string]string{"package.json": `{"scripts":{"db:seed":"node seed.js"}}`}, true},
		{"unrelated seedrandom", map[string]string{"seedrandom.js": "// rng"}, false},
		{"none", map[string]string{"main.go": "package main"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range tc.files {
				write(t, dir, rel, body)
			}
			_, ok := hasSeed(openLocal(t, dir))
			if ok != tc.want {
				t.Errorf("hasSeed(%v) = %v, want %v", tc.files, ok, tc.want)
			}
		})
	}
}

// The trio composes additively: a compose DB service and seed data round the
// core (migrations) up to full marks.
func TestDBProvisioningTrioScoring(t *testing.T) {
	compose := "services:\n  db:\n    image: postgres:16\n"
	migration := map[string]string{"migrations/001_init.sql": "create table t();"}
	seed := "insert into t values ();"

	full := scoreDir(t, writeRepo(t, map[string]string{
		"go.mod":                  "module x\n",
		"docker-compose.yml":      compose,
		"migrations/001_init.sql": migration["migrations/001_init.sql"],
		"db/seed.sql":             seed,
	}))
	if got := cat(full, scorecard.CatDBMigrations).Normalized; got != 1 {
		t.Errorf("full trio => 1.0, got %v", got)
	}

	// Compose DB service alone (no migrations, no seed): applicable, partial.
	bringUpOnly := scoreDir(t, writeRepo(t, map[string]string{"docker-compose.yml": compose}))
	m := cat(bringUpOnly, scorecard.CatDBMigrations)
	if !m.Applicable {
		t.Fatal("a compose DB service should make db-migrations applicable")
	}
	if m.Normalized != dbProvisionBonus {
		t.Errorf("bring-up only => %v, got %v", dbProvisionBonus, m.Normalized)
	}
}
