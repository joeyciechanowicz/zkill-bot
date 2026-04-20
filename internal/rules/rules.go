package rules

import (
	"sort"

	"zkill-bot/internal/killmail"
)

// Mode controls how many rules are evaluated once a match is found.
type Mode string

const (
	ModeFirstMatch Mode = "first-match"
	ModeMultiMatch Mode = "multi-match"
)

// ActionConfig holds the action type and optional per-rule arguments.
type ActionConfig struct {
	Type string                 `yaml:"type"`
	Args map[string]interface{} `yaml:"args"`
}

// Rule is a single configured rule with its filter tree and actions.
type Rule struct {
	Name     string         `yaml:"name"`
	Enabled  bool           `yaml:"enabled"`
	Priority int            `yaml:"priority"`
	Filter   FilterNode     `yaml:"filter"`
	Actions  []ActionConfig `yaml:"actions"`
}

// RuleFile holds the evaluation mode and the list of rules.
// It is unmarshalled directly by the config package as part of config.yaml.
type RuleFile struct {
	Mode  Mode   `yaml:"mode"`
	Rules []Rule `yaml:"rules"`
}

// RuleMatch is returned by Evaluate for each matched rule.
type RuleMatch struct {
	Rule    *Rule
	Actions []ActionConfig
}

// ExtractSolarSystemNames returns all unique solar_system_name values referenced
// in the filter trees of enabled rules.
func ExtractSolarSystemNames(rf *RuleFile) []string {
	seen := map[string]bool{}
	var out []string
	for i := range rf.Rules {
		if !rf.Rules[i].Enabled {
			continue
		}
		collectSystemNames(&rf.Rules[i].Filter, seen, &out)
	}
	return out
}

func collectSystemNames(f *FilterNode, seen map[string]bool, out *[]string) {
	for _, name := range f.SolarSystemName {
		if !seen[name] {
			seen[name] = true
			*out = append(*out, name)
		}
	}
	for _, child := range f.And {
		collectSystemNames(child, seen, out)
	}
	for _, child := range f.Or {
		collectSystemNames(child, seen, out)
	}
	if f.Not != nil {
		collectSystemNames(f.Not, seen, out)
	}
}

// Evaluate tests km against all enabled rules in priority order.
// In first-match mode it returns as soon as one rule matches.
// In multi-match mode it returns all matching rules.
func Evaluate(km *killmail.Killmail, rf *RuleFile) []RuleMatch {
	// Sort by priority each time so callers don't need to pre-sort.
	sorted := make([]Rule, len(rf.Rules))
	copy(sorted, rf.Rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	var matches []RuleMatch
	for i := range sorted {
		r := &sorted[i]
		if !r.Enabled {
			continue
		}
		if matchFilter(km, &r.Filter) {
			matches = append(matches, RuleMatch{Rule: r, Actions: r.Actions})
			if rf.Mode == ModeFirstMatch {
				return matches
			}
		}
	}
	return matches
}
