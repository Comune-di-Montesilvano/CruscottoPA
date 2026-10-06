// Package icons espone il catalogo dei nomi Material Icons (ligature del font
// in web/static/fonts), usato per validare le icone delle app e per il picker admin.
package icons

import (
	_ "embed"
	"sort"
	"strings"
)

//go:embed codepoints.txt
var codepoints string

var (
	names []string
	set   = map[string]struct{}{}
)

func init() {
	for _, line := range strings.Split(codepoints, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if _, dup := set[f[0]]; !dup {
			set[f[0]] = struct{}{}
			names = append(names, f[0])
		}
	}
	sort.Strings(names)
}

// Valid indica se name è un'icona del catalogo.
func Valid(name string) bool {
	_, ok := set[name]
	return ok
}

// Count restituisce il numero di icone nel catalogo.
func Count() int { return len(names) }

// Search restituisce al massimo limit nomi: prima quelli che iniziano con q,
// poi quelli che lo contengono. Gli spazi in q valgono come underscore.
func Search(q string, limit int) []string {
	q = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(q)), " ", "_")
	var prefix, contains []string
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, q):
			prefix = append(prefix, n)
		case strings.Contains(n, q):
			contains = append(contains, n)
		}
		if len(prefix) >= limit {
			break
		}
	}
	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
