// SPDX-License-Identifier: Apache-2.0

// Package config resolves toaster-ready's configuration: the maintainer's opinionated
// defaults (rubric v2) with any overrides from a repo's .toaster-ready.yml applied on top.
// The contract is "sane defaults with full override" — with no config file a repo
// is scored exactly by the built-in defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/tittle-xyz/toaster-ready/internal/ctxbudget"
	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

// ContextBudget overrides the always-loaded token thresholds.
type ContextBudget struct {
	Soft int `yaml:"soft" json:"soft"`
	Hard int `yaml:"hard" json:"hard"`
}

// Gate holds gate settings. Threshold is the minimum /100 score to pass; it is
// consumed by `toaster gate`.
type Gate struct {
	Threshold float64 `yaml:"threshold" json:"threshold"`
}

// Recommend controls recommendation generation: a category gets advice when
// its normalized subscore is below Below.
type Recommend struct {
	Below float64 `yaml:"below" json:"below"`
}

// Config is the resolved configuration used to score a repo.
type Config struct {
	Weights       map[string]float64 `yaml:"weights" json:"weights"`               // category id -> relative weight
	Disabled      []string           `yaml:"disabled" json:"disabled,omitempty"`   // category ids to skip
	Languages     []string           `yaml:"languages" json:"languages,omitempty"` // stack hints that augment detection
	ContextBudget ContextBudget      `yaml:"contextBudget" json:"contextBudget"`
	Gate          Gate               `yaml:"gate" json:"gate"`
	Recommend     Recommend          `yaml:"recommend" json:"recommend"`
}

// Default returns the built-in configuration: rubric-v2 default weights, the
// default context budget, and the baseline gate threshold (functional floor).
func Default() Config {
	w := make(map[string]float64, len(scorecard.DefaultRubricV2))
	for _, d := range scorecard.DefaultRubricV2 {
		w[d.ID] = d.Weight
	}
	return Config{
		Weights:       w,
		ContextBudget: ContextBudget{Soft: ctxbudget.SoftBudgetTokens, Hard: ctxbudget.HardBudgetTokens},
		Gate:          Gate{Threshold: 50},
		Recommend:     Recommend{Below: 0.75},
	}
}

// WeightFor returns the configured weight for a category id (0 if unset).
func (c Config) WeightFor(id string) float64 { return c.Weights[id] }

// IsDisabled reports whether a category id is turned off.
func (c Config) IsDisabled(id string) bool {
	for _, d := range c.Disabled {
		if d == id {
			return true
		}
	}
	return false
}

// Filenames searched at the repo root, in order.
var Filenames = []string{".toaster-ready.yml", ".toaster-ready.yaml"}

// fileConfig mirrors Config but with pointer scalars so an override file can set
// only the parts it cares about — and so an explicit 0 (e.g. gate.threshold: 0)
// is distinguishable from "unset" and honored, while a bare block keeps defaults.
type fileConfig struct {
	Weights       map[string]float64 `yaml:"weights"`
	Disabled      []string           `yaml:"disabled"`
	Languages     []string           `yaml:"languages"`
	ContextBudget *struct {
		Soft *int `yaml:"soft"`
		Hard *int `yaml:"hard"`
	} `yaml:"contextBudget"`
	Gate *struct {
		Threshold *float64 `yaml:"threshold"`
	} `yaml:"gate"`
	Recommend *struct {
		Below *float64 `yaml:"below"`
	} `yaml:"recommend"`
}

func (f fileConfig) applyTo(c *Config) {
	for id, w := range f.Weights {
		c.Weights[id] = w
	}
	if f.Disabled != nil {
		c.Disabled = f.Disabled
	}
	if f.Languages != nil {
		c.Languages = f.Languages
	}
	if f.ContextBudget != nil {
		if f.ContextBudget.Soft != nil {
			c.ContextBudget.Soft = *f.ContextBudget.Soft
		}
		if f.ContextBudget.Hard != nil {
			c.ContextBudget.Hard = *f.ContextBudget.Hard
		}
	}
	if f.Gate != nil && f.Gate.Threshold != nil {
		c.Gate.Threshold = *f.Gate.Threshold
	}
	if f.Recommend != nil && f.Recommend.Below != nil {
		c.Recommend.Below = *f.Recommend.Below
	}
}

// Load returns the default config merged with a .toaster-ready.yml if present. An explicit
// path (from --config) takes precedence over auto-discovery at root; an explicit
// path that doesn't exist is an error, while a missing auto-discovered file is not.
// The returned string is the config path used ("" if none).
func Load(root, explicit string) (Config, string, error) {
	cfg := Default()

	path := explicit
	if path == "" {
		for _, name := range Filenames {
			p := filepath.Join(root, name)
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			return cfg, "", nil
		}
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, "", err
	}
	var f fileConfig
	if err := yaml.Unmarshal(b, &f); err != nil {
		return cfg, path, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validate(f); err != nil {
		return cfg, path, fmt.Errorf("%s: %w", path, err)
	}
	f.applyTo(&cfg)
	return cfg, path, nil
}

// validate rejects references to unknown category ids — typos shouldn't silently
// no-op. The category set is fixed (ADR-0002): config tunes it, never extends it.
func validate(f fileConfig) error {
	var unknown []string
	for id := range f.Weights {
		if !scorecard.KnownCategory(id) {
			unknown = append(unknown, id)
		}
	}
	for _, id := range f.Disabled {
		if !scorecard.KnownCategory(id) {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown category id(s): %v", unknown)
	}
	return nil
}
