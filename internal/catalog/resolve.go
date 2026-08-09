package catalog

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// aliases map names people actually type to upstream template names.
// Keys are lowercase.
var aliases = map[string]string{
	"vscode":   "VisualStudioCode",
	"vs-code":  "VisualStudioCode",
	"vs":       "VisualStudio",
	"golang":   "Go",
	"py":       "Python",
	"osx":      "macOS",
	"mac":      "macOS",
	"win":      "Windows",
	"idea":     "JetBrains",
	"intellij": "JetBrains",
	"pycharm":  "JetBrains",
	"goland":   "JetBrains",
	"webstorm": "JetBrains",
	"clion":    "JetBrains",
	"rider":    "JetBrains",
	"js":       "Node",
	"nodejs":   "Node",
	"cpp":      "C++",
	"tex":      "TeX",
	"latex":    "TeX",
}

// UnknownNameError reports a name with no match, along with close spellings.
type UnknownNameError struct {
	Name        string
	Suggestions []string
}

func (e *UnknownNameError) Error() string {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("unknown template %q", e.Name))
	if len(e.Suggestions) > 0 {
		msg.WriteString("\n\nDid you mean:\n")
		for _, s := range e.Suggestions {
			msg.WriteString("  " + s + "\n")
		}
		msg.WriteString("\nRun `igo list` to see everything available.")
	} else {
		msg.WriteString(" (run `igo list` to see what is available)")
	}
	return msg.String()
}

// AmbiguousNameError reports a base name shared by several upstream paths
// that precedence could not separate.
type AmbiguousNameError struct {
	Name       string
	Candidates []string
}

func (e *AmbiguousNameError) Error() string {
	return fmt.Sprintf("%q matches several templates: %s (use the full path)",
		e.Name, strings.Join(e.Candidates, ", "))
}

// Resolve maps a user-supplied name to an upstream template path.
//
// It accepts an alias, a full path such as "Global/Vim", or a bare base
// name. Matching is case-insensitive throughout. When a base name appears
// in several directories, a shallower path wins, which is what makes
// "Racket" mean the top-level template rather than the community one.
func (c *Catalog) Resolve(name string) (string, error) {
	query := strings.ToLower(strings.TrimSpace(name))
	if query == "" {
		return "", fmt.Errorf("empty template name")
	}
	if alias, ok := aliases[query]; ok {
		query = strings.ToLower(alias)
	}
	query = strings.TrimSuffix(query, strings.ToLower(ext))

	if match, ok := c.byPath[query]; ok {
		return match, nil
	}

	candidates := c.byBase[query]
	switch len(candidates) {
	case 0:
		return "", &UnknownNameError{Name: name, Suggestions: c.suggest(query)}
	case 1:
		return candidates[0], nil
	}

	shallowest := slices.MinFunc(candidates, func(a, b string) int {
		return strings.Count(a, "/") - strings.Count(b, "/")
	})
	tied := 0
	for _, candidate := range candidates {
		if strings.Count(candidate, "/") == strings.Count(shallowest, "/") {
			tied++
		}
	}
	if tied > 1 {
		return "", &AmbiguousNameError{Name: name, Candidates: candidates}
	}
	return shallowest, nil
}

// suggest returns up to 5 template names spelled close to query, nearest
// first. Substring matches come first since those are usually what the
// user meant when they typed a fragment.
func (c *Catalog) suggest(query string) []string {
	type scored struct {
		name string
		dist int
	}
	var matches []scored

	for _, upstreamPath := range c.index {
		base := strings.ToLower(path.Base(upstreamPath))
		dist := editDistance(query, base)
		if strings.Contains(base, query) && len(query) >= 3 {
			dist = 0
		}
		if dist <= maxDistance(query) {
			matches = append(matches, scored{c.displayName(upstreamPath), dist})
		}
	}

	slices.SortFunc(matches, func(a, b scored) int {
		if a.dist != b.dist {
			return a.dist - b.dist
		}
		return strings.Compare(a.name, b.name)
	})

	names := make([]string, 0, 5)
	for _, m := range matches[:min(len(matches), 5)] {
		names = append(names, m.name)
	}
	return names
}

// maxDistance keeps suggestions tight for short names, where a distance of
// 2 would match almost anything.
func maxDistance(query string) int {
	switch {
	case len(query) <= 3:
		return 1
	case len(query) <= 6:
		return 2
	default:
		return 3
	}
}

// editDistance is Levenshtein distance over runes, using two rows rather
// than a full matrix.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}

	prev := make([]int, len(ar)+1)
	curr := make([]int, len(ar)+1)
	for i := range prev {
		prev[i] = i
	}

	for j := 1; j <= len(br); j++ {
		curr[0] = j
		for i := 1; i <= len(ar); i++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[i] = min(curr[i-1]+1, prev[i]+1, prev[i-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(ar)]
}
