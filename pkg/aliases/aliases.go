// Package aliases resolves model aliases to the latest available version
// by querying provider model listing APIs.
package aliases

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"godex/pkg/backend"
)

// Rule defines how an alias maps to a model family.
// The resolver queries the specified backend and picks the latest model
// matching the prefix.
type Rule struct {
	Alias   string // e.g. "opus"
	Prefix  string // e.g. "claude-opus-" — pick latest model starting with this
	Backend string // backend name to query (e.g. "anthropic", "gemini")
}

// DefaultRules returns the built-in alias resolution rules.
func DefaultRules() []Rule {
	return []Rule{
		// Anthropic
		{Alias: "opus", Prefix: "claude-opus-", Backend: "anthropic"},
		{Alias: "sonnet", Prefix: "claude-sonnet-", Backend: "anthropic"},
		{Alias: "haiku", Prefix: "claude-haiku-", Backend: "anthropic"},

		// Gemini
		{Alias: "gemini", Prefix: "gemini-2.5-pro", Backend: "gemini"},
		{Alias: "flash", Prefix: "gemini-2.5-flash", Backend: "gemini"},
	}
}

// Resolution is the result of resolving one alias.
type Resolution struct {
	Alias    string
	Previous string // old value (empty if new)
	Resolved string // new value
	Changed  bool
	Error    string // non-empty if resolution failed
}

// Resolve queries the given backends and resolves aliases to the latest model.
// backends is a map of backend name → Backend instance.
// current is the existing alias map (may be nil).
// rules defines which aliases to resolve; if nil, DefaultRules() is used.
func Resolve(ctx context.Context, backends map[string]backend.Backend, current map[string]string, rules []Rule) []Resolution {
	if rules == nil {
		rules = DefaultRules()
	}
	if current == nil {
		current = map[string]string{}
	}

	// Cache model lists per backend
	modelCache := map[string][]backend.ModelInfo{}

	var results []Resolution
	for _, rule := range rules {
		res := Resolution{
			Alias:    rule.Alias,
			Previous: current[rule.Alias],
		}

		be, ok := backends[rule.Backend]
		if !ok {
			res.Error = fmt.Sprintf("backend %q not available", rule.Backend)
			res.Resolved = res.Previous // keep old value
			results = append(results, res)
			continue
		}

		models, cached := modelCache[rule.Backend]
		if !cached {
			var err error
			models, err = be.ListModels(ctx)
			if err != nil {
				res.Error = fmt.Sprintf("list models: %v", err)
				res.Resolved = res.Previous
				results = append(results, res)
				continue
			}
			modelCache[rule.Backend] = models
		}

		resolved := pickLatest(models, rule.Prefix)
		if resolved == "" {
			res.Error = fmt.Sprintf("no model matching prefix %q", rule.Prefix)
			res.Resolved = res.Previous
		} else {
			res.Resolved = resolved
			res.Changed = res.Previous != resolved
		}
		results = append(results, res)
	}
	return results
}

// pickLatest finds the latest model matching the given prefix.
// It sorts matching models lexicographically descending (higher version numbers
// and later dates sort later) and returns the last one.
func pickLatest(models []backend.ModelInfo, prefix string) string {
	var matches []string
	for _, m := range models {
		if strings.HasPrefix(m.ID, prefix) {
			matches = append(matches, m.ID)
		}
	}
	if len(matches) == 0 {
		// Exact match fallback (e.g. prefix "gemini-2.5-pro" matches "gemini-2.5-pro")
		for _, m := range models {
			if m.ID == prefix {
				return m.ID
			}
		}
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// ApplyResolutions updates the alias map with successful resolutions.
// Returns the number of aliases changed.
func ApplyResolutions(aliases map[string]string, resolutions []Resolution) int {
	changed := 0
	for _, r := range resolutions {
		if r.Error == "" && r.Resolved != "" {
			if aliases[r.Alias] != r.Resolved {
				aliases[r.Alias] = r.Resolved
				changed++
			}
		}
	}
	return changed
}
